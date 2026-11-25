package migrations_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/migrations"
)

// orderRow is an order to insert; a nil field is NULL, and a shipment's
// columns come with mode.
type orderRow struct {
	status                string
	price                 int64
	mode, by, reason, end *string
	shipped, closed       *time.Time
}

// insertOrder puts r on a new deal of buyer's on a new listing of seller's.
func insertOrder(ctx context.Context, t *testing.T, db *pgxpool.Pool, buyer, seller uuid.UUID, r orderRow) (uuid.UUID, error) {
	t.Helper()
	deal, order := uuid.NewV7(), uuid.NewV7()
	a := pgx.NamedArgs{"deal": deal, "listing": listing(t, db, seller), "buyer": buyer, "seller": seller}
	if _, err := db.Exec(ctx, insertPairDeal, a); err != nil {
		t.Fatal(err)
	}
	var carrier, code, state any
	if r.mode != nil {
		carrier, code, state = "ghtk", order.String(), "in_transit"
	}
	a["order"], a["status"], a["price"], a["mode"], a["carrier"], a["code"], a["state"] = order, r.status, r.price, r.mode, carrier, code, state
	a["by"], a["reason"], a["shipped"], a["closed"], a["end"] = r.by, r.reason, r.shipped, r.closed, r.end
	_, err := db.Exec(ctx, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price,
    recipient_name, recipient_phone, recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier,
    tracking_code, shipment_status, cancelled_by, cancel_reason, completed_by, requested_at, shipped_at, closed_at)
VALUES (@order, @deal, @listing, @buyer, @seller, @status, @price, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    @mode, @carrier, @code, @state, @by, @reason, @end, now(), @shipped, @closed)`, a)
	return order, err
}

// broke reports whether err is a CHECK named name.
func broke(err error, name string) bool {
	e, ok := errors.AsType[*pgconn.PgError](err)
	return ok && e.Code == "23514" && e.ConstraintName == name
}

// 0097 checks new and updated orders and deals against the rules module
// deal holds, and leaves an old row that breaks them in place.
func TestOrderChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0097_order_checks.sql")); err != nil {
		t.Fatal(err)
	}
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	old, err := insertOrder(ctx, t, db, buyer, seller, orderRow{status: "pending", price: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("an old row failed the migration: %v", err)
	}
	if _, err = db.Exec(ctx, `UPDATE orders SET recipient_name = '' WHERE id = @id`, pgx.NamedArgs{"id": old}); !broke(err, "orders_price_check") {
		t.Fatalf("an old row updated: %v", err)
	}

	manual, buyerParty := new("manual"), new("buyer")
	for name, c := range map[string]struct {
		row  orderRow
		want string
	}{
		"pending":                   {orderRow{status: "pending", price: 1_500_000}, ""},
		"cancelled":                 {orderRow{status: "cancelled", price: 1_500_000, by: buyerParty, reason: buyerParty, closed: &at}, ""},
		"completed":                 {orderRow{status: "completed", price: 1_500_000, mode: manual, shipped: &at, closed: &at, end: buyerParty}, ""},
		"free":                      {orderRow{status: "pending", price: 0}, "orders_price_check"},
		"cancelled without why":     {orderRow{status: "cancelled", price: 1_500_000, by: buyerParty, closed: &at}, "orders_cancel_check"},
		"pending with a canceller":  {orderRow{status: "pending", price: 1_500_000, by: buyerParty, reason: buyerParty}, "orders_cancel_check"},
		"failed open":               {orderRow{status: "failed", price: 1_500_000, mode: manual, shipped: &at}, "orders_closed_check"},
		"pending closed":            {orderRow{status: "pending", price: 1_500_000, closed: &at}, "orders_closed_check"},
		"shipped without a time":    {orderRow{status: "shipped", price: 1_500_000, mode: manual}, "orders_shipped_check"},
		"a time without a shipment": {orderRow{status: "pending", price: 1_500_000, shipped: &at}, "orders_shipped_check"},
	} {
		_, err = insertOrder(ctx, t, db, buyer, seller, c.row)
		if (c.want == "" && err != nil) || (c.want != "" && !broke(err, c.want)) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}

	deal := uuid.NewV7()
	if _, err = db.Exec(ctx, insertPairDeal, pgx.NamedArgs{"deal": deal, "listing": listing(t, db, seller), "buyer": buyer, "seller": seller}); err != nil {
		t.Fatal(err)
	}
	for set, want := range map[string]string{
		`open_by = 'buyer', open_amount = 1200000, open_at = now()`:                                                               "",
		`open_by = 'buyer', open_amount = 0, open_at = now()`:                                                                     "deals_open_amount_check",
		`status = 'agreed', agreed_price = 1200000, agreed_at = now(), open_by = 'buyer', open_amount = 1200000, open_at = now()`: "deals_open_status_check",
	} {
		_, err = db.Exec(ctx, `UPDATE deals SET `+set+` WHERE id = @id`, pgx.NamedArgs{"id": deal})
		if (want == "" && err != nil) || (want != "" && !broke(err, want)) {
			t.Errorf("%s: %v, want %q", set, err, want)
		}
	}
}
