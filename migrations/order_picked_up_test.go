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

// 0071 gives a parcel on its way the time of the carrier's first update,
// the seller's correction aside, or the shipment's when no update came; a
// parcel waiting for pickup keeps none.
func TestOrderPickedUpTakesTheFirstCarrierUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0071_order_picked_up.sql")); err != nil {
		t.Fatal(err)
	}
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	orders := map[string]struct {
		shipment string
		want     *time.Time
	}{
		"on its way":  {"in_transit", new(at.Add(2 * time.Hour))},
		"delivered":   {"delivered", new(at)},
		"not yet out": {"pending_pickup", nil},
	}
	ids := map[string]uuid.UUID{}
	for name, o := range orders {
		ids[name] = uuid.NewV7()
		a := pgx.NamedArgs{
			"deal": uuid.NewV7(), "listing": listing(t, db, seller), "buyer": buyer, "seller": seller, "order": ids[name],
			"shipment": o.shipment, "at": at, "code": "GHN-" + o.shipment,
		}
		for _, q := range []string{insertPairDeal, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price,
    recipient_name, recipient_phone, recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier,
    tracking_code, shipment_status, requested_at, shipped_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    'manual', 'ghn', @code, @shipment, @at, @at)`} {
			if _, err := db.Exec(ctx, q, a); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	}
	for by, moved := range map[string]time.Time{"seller": at.Add(time.Hour), "system": at.Add(2 * time.Hour)} {
		if _, err := db.Exec(ctx, `INSERT INTO deal_moves (id, deal_id, kind, by_party, moved_at, order_id)
SELECT uuidv7(), deal_id, 'shipment_updated', @by, @moved, id FROM orders WHERE id = @order`,
			pgx.NamedArgs{"by": by, "moved": moved, "order": ids["on its way"]}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for name, o := range orders {
		var got *time.Time
		if err := db.QueryRow(ctx, `SELECT picked_up_at FROM orders WHERE id = @id`, pgx.NamedArgs{"id": ids[name]}).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (o.want == nil) || (got != nil && !got.Equal(*o.want)) {
			t.Errorf("%s: picked up at %v, want %v", name, got, o.want)
		}
	}
}
