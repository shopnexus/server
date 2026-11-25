package migrations_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/migrations"
)

// 0092 gives a sold listing with a completed order that order; a listing
// marked sold by hand, or one on sale after a completed order, keeps none.
func TestListingSoldOrderBackfill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0092_listing_sold_order.sql")); err != nil {
		t.Fatal(err)
	}
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	sold, hand, onSale := listing(t, db, seller), listing(t, db, seller), listing(t, db, seller)
	orders := map[uuid.UUID]uuid.UUID{sold: uuid.NewV7(), onSale: uuid.NewV7()}
	for l, order := range orders {
		a := pgx.NamedArgs{"deal": uuid.NewV7(), "listing": l, "buyer": buyer, "seller": seller, "order": order, "at": at, "code": order.String()}
		for _, q := range []string{insertPairDeal, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price,
    recipient_name, recipient_phone, recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier,
    tracking_code, shipment_status, requested_at, shipped_at, delivered_at, closed_at, completed_by)
VALUES (@order, @deal, @listing, @buyer, @seller, 'completed', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    'manual', 'ghn', @code, 'delivered', @at, @at, @at, @at, 'buyer')`} {
			if _, err := db.Exec(ctx, q, a); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	}
	if _, err := db.Exec(ctx, `UPDATE listings SET status = 'sold', sold_at = @at WHERE id = ANY(@ids)`,
		pgx.NamedArgs{"at": at, "ids": []uuid.UUID{sold, hand}}); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for l, want := range map[uuid.UUID]*uuid.UUID{sold: new(orders[sold]), hand: nil, onSale: nil} {
		var got *uuid.UUID
		if err := db.QueryRow(ctx, `SELECT sold_order_id FROM listings WHERE id = @id`, pgx.NamedArgs{"id": l}).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("listing %s: sold order %v, want %v", l, got, want)
		}
	}
	// Only a sold or deleted listing names its order.
	if _, err := db.Exec(ctx, `UPDATE listings SET sold_order_id = @order WHERE id = @id`,
		pgx.NamedArgs{"order": orders[onSale], "id": onSale}); err == nil {
		t.Fatal("an active listing took a sold order")
	}
}
