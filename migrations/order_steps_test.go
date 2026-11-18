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

// A deal of the release before had an order cancelled and a second one
// shipped. 0052 gives each step of an order the order whose time it
// carries, and each order notice the order of its step; a step or a notice
// that matches no order keeps none.
func TestOrderStepsNameTheirOrders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0052_order_steps.sql")); err != nil {
		t.Fatal(err)
	}
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	a := pgx.NamedArgs{
		"deal": uuid.NewV7(), "listing": listing(t, db, seller), "buyer": buyer, "seller": seller,
		"first": uuid.NewV7(), "second": uuid.NewV7(),
		"t1": at, "t2": at.Add(time.Hour), "t3": at.Add(2 * time.Hour), "t4": at.Add(3 * time.Hour),
		"t5": at.Add(4 * time.Hour), "t9": at.Add(9 * time.Hour),
	}
	moves := map[string]uuid.UUID{}
	notices := map[string]uuid.UUID{}
	steps := make([]string, 0, 16)
	steps = append(steps,
		insertPairDeal,
		`INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
    recipient_street, recipient_commune, recipient_kind, cancelled_by, cancel_reason, requested_at, closed_at)
VALUES (@first, @deal, @listing, @buyer, @seller, 'cancelled', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    'buyer', 'buyer', @t1, @t2)`,
		`INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
    recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier, tracking_code, shipment_status,
    requested_at, shipped_at)
VALUES (@second, @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    'manual', 'ghn', '8A2K9', 'in_transit', @t3, @t4)`,
	)
	for name, m := range map[string]struct{ kind, at string }{
		"first request": {"cod_requested", "t1"}, "first cancel": {"order_cancelled", "t2"},
		"second request": {"cod_requested", "t3"}, "second shipment": {"shipped", "t4"},
		"second update": {"shipment_updated", "t5"}, "accept": {"accept", "t2"}, "no order's time": {"completed", "t9"},
	} {
		moves[name] = uuid.NewV7()
		a["move_"+m.at+m.kind] = moves[name]
		steps = append(steps, `INSERT INTO deal_moves (id, deal_id, kind, by_party, moved_at)
VALUES (@move_`+m.at+m.kind+`, @deal, '`+m.kind+`', 'buyer', @`+m.at+`)`)
	}
	for name, n := range map[string]struct{ kind, at string }{
		"first request": {"cod_requested", "t1"}, "first cancel": {"order_cancelled", "t2"},
		"second request": {"cod_requested", "t3"}, "second shipment": {"shipped", "t4"},
		"offer": {"offer", "t1"}, "no step": {"failed", "t9"},
	} {
		notices[name] = uuid.NewV7()
		a["notice_"+n.at+n.kind] = notices[name]
		steps = append(steps, `INSERT INTO notifications (id, account_id, notice_type, collapse_key, params, event_id,
    created_at, bumped_at)
VALUES (@notice_`+n.at+n.kind+`, @seller, '`+n.kind+`', NULL, jsonb_build_object('deal_id', @deal::text, 'title', 'Xe đạp'),
    uuidv7(), @`+n.at+`, @`+n.at+`)`)
	}
	for _, q := range steps {
		if _, err := db.Exec(ctx, q, a); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}

	first, second, none := a["first"].(uuid.UUID).String(), a["second"].(uuid.UUID).String(), ""
	for name, want := range map[string]string{
		"first request": first, "first cancel": first, "second request": second, "second shipment": second,
		"second update": second, "accept": none, "no order's time": none,
	} {
		var got string
		if err := db.QueryRow(ctx, `SELECT coalesce(order_id::text, '') FROM deal_moves WHERE id = @id`,
			pgx.StrictNamedArgs{"id": moves[name]}).Scan(&got); err != nil || got != want {
			t.Errorf("move %s: order %q, want %q, %v", name, got, want, err)
		}
	}
	for name, want := range map[string]string{
		"first request": first, "first cancel": first, "second request": second, "second shipment": second,
		"offer": none, "no step": none,
	} {
		var got string
		if err := db.QueryRow(ctx, `SELECT coalesce(params ->> 'order_id', '') FROM notifications WHERE id = @id`,
			pgx.StrictNamedArgs{"id": notices[name]}).Scan(&got); err != nil || got != want {
			t.Errorf("notice %s: order %q, want %q, %v", name, got, want, err)
		}
	}
}
