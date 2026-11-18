package migrations_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/migrations"
)

// 0050 gives each report of an order the order's seller, and none to a
// ticket without an order.
func TestConsoleAgentFillsTicketSellers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0050_console_agent.sql")); err != nil {
		t.Fatal(err)
	}
	person, seller := accounttest.New(t, db), accounttest.New(t, db)
	order := uuid.NewV7()
	a := pgx.NamedArgs{"deal": uuid.NewV7(), "order": order, "listing": listing(t, db, seller), "buyer": person, "seller": seller}
	for _, q := range []string{insertPairDeal, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name,
    recipient_phone, recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier, tracking_code, shipment_status, requested_at, shipped_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    'manual', 'ghtk', 'S1.A1', 'in_transit', now(), now())`} {
		if _, err := db.Exec(ctx, q, a); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	report, other := uuid.NewV7(), uuid.NewV7()
	for id, o := range map[uuid.UUID]*uuid.UUID{report: &order, other: nil} {
		conv := uuid.NewV7()
		if _, err := db.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, kind, created_at)
VALUES (@id, LEAST(@p::uuid, @s::uuid), GREATEST(@p::uuid, @s::uuid), 'support', now())`,
			pgx.StrictNamedArgs{"id": conv, "p": person, "s": uuid.MustParse(shopNexus)}); err != nil {
			t.Fatal(err)
		}
		kind := "other"
		if o != nil {
			kind = "not_received"
		}
		if _, err := db.Exec(ctx, `INSERT INTO tickets (id, account_id, conversation_id, kind, status, order_id, last_message_at,
    last_from, opened_at, request_key)
VALUES (@id, @person, @conv, @kind, 'open', @order, now(), 'person', now(), uuidv7())`,
			pgx.StrictNamedArgs{"id": id, "person": person, "conv": conv, "kind": kind, "order": o}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("0050 on a database with tickets: %v", err)
	}
	for id, want := range map[uuid.UUID]*uuid.UUID{report: &seller, other: nil} {
		var got *uuid.UUID
		if err := db.QueryRow(ctx, `SELECT seller_id FROM tickets WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("ticket %s: seller %v, want %v", id, got, want)
		}
	}
}
