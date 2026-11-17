package migrations_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/migrations"
)

// The addresses and orders of the release before become home, and a kind
// outside home, work and other does not go in.
func TestAddressKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0046_address_kind.sql")); err != nil {
		t.Fatal(err)
	}
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	address, order := uuid.NewV7(), uuid.NewV7()
	a := pgx.NamedArgs{
		"address": address, "deal": uuid.NewV7(), "order": order, "listing": listing(t, db, seller),
		"buyer": buyer, "seller": seller,
	}
	for _, q := range []string{
		`INSERT INTO addresses (id, account_id, recipient_name, phone, street, commune_code, is_default, request_key, write_id, created_at)
VALUES (@address, @buyer, 'Hân', '+84912345678', '1 Lê Lợi', '00004', true, gen_random_uuid(), @address, now())`,
		insertDeal,
		`INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
    recipient_street, recipient_commune, requested_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'pending', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', now())`,
	} {
		if _, err := db.Exec(ctx, q, a); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var addressKind, recipientKind string
	if err := db.QueryRow(ctx, `SELECT kind FROM addresses WHERE id = @id`, pgx.StrictNamedArgs{"id": address}).
		Scan(&addressKind); err != nil || addressKind != "home" {
		t.Fatalf("an address from before: %q %v", addressKind, err)
	}
	if err := db.QueryRow(ctx, `SELECT recipient_kind FROM orders WHERE id = @id`, pgx.StrictNamedArgs{"id": order}).
		Scan(&recipientKind); err != nil || recipientKind != "home" {
		t.Fatalf("an order from before: %q %v", recipientKind, err)
	}
	if _, err := db.Exec(ctx, `UPDATE addresses SET kind = 'office' WHERE id = @id`, pgx.StrictNamedArgs{"id": address}); err == nil {
		t.Error("an address took the kind office")
	}
	if _, err := db.Exec(ctx, `UPDATE orders SET recipient_kind = 'office' WHERE id = @id`, pgx.StrictNamedArgs{"id": order}); err == nil {
		t.Error("an order took the kind office")
	}
	// The default only filled the rows from before: an insert names the kind.
	if _, err := db.Exec(ctx, `INSERT INTO addresses (id, account_id, recipient_name, phone, street, commune_code, is_default, request_key, write_id, created_at)
VALUES (@id, @buyer, 'Hân', '+84912345678', '1 Lê Lợi', '00004', false, @id, @id, now())`,
		pgx.NamedArgs{"id": uuid.NewV7(), "buyer": buyer}); err == nil {
		t.Error("an address went in without a kind")
	}
}
