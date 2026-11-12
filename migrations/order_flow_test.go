package migrations_test

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/migrations"
)

// null is s, or NULL for the empty string.
func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

const insertDeal = `INSERT INTO deals (id, listing_id, buyer_id, seller_id, status, listed_price, write_id, created_at, updated_at)
VALUES (@deal, @listing, @buyer, @seller, 'negotiating', 1500000, @deal, now(), now())`

// The orders, moves and notices of the release before arrive under the
// new names: requested is pending, delivered completed, returned failed, a
// waybill a manual shipment, and a waybill of carrier other no shipment.
func TestOrderFlowRenamesTheOldOrders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0040_order_flow.sql")); err != nil {
		t.Fatal(err)
	}
	// 0040 leaves accounts and listings as 0039 has them, so today's helpers fit.
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	olds := map[string]struct{ status, carrier, carrierName, reason string }{
		"requested": {status: "requested"},
		"shipped":   {status: "shipped", carrier: "spx"},
		"other":     {status: "shipped", carrier: "other", carrierName: "Ahamove"},
		"delivered": {status: "delivered", carrier: "ghn"},
		"returned":  {status: "returned", carrier: "jt"},
		"cancelled": {status: "cancelled", reason: "sent_to_another"},
	}
	orders, deals := map[string]uuid.UUID{}, map[string]uuid.UUID{}
	for name, o := range olds {
		orders[name], deals[name] = uuid.NewV7(), uuid.NewV7()
		a := pgx.NamedArgs{
			"deal": deals[name], "order": orders[name], "listing": catalogtest.Listing(t, db, seller), "buyer": buyer,
			"seller": seller, "status": o.status, "source": nil, "carrier": null(o.carrier), "carrier_name": null(o.carrierName),
			"code": nil, "fee": nil, "by": nil, "reason": null(o.reason), "shipped_at": nil, "closed_at": nil,
		}
		if o.carrier != "" {
			a["source"], a["code"], a["fee"], a["shipped_at"] = "self", "CODE-"+name, 22_000, time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
		}
		if o.status != "requested" && o.status != "shipped" {
			a["closed_at"] = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
		}
		if o.reason != "" {
			a["by"] = "system"
		}
		for _, q := range []string{insertDeal, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price,
    recipient_name, recipient_phone, recipient_street, recipient_commune, waybill_source, carrier, carrier_name, tracking_code,
    fee, cancelled_by, cancel_reason, requested_at, shipped_at, closed_at)
VALUES (@order, @deal, @listing, @buyer, @seller, @status, 1500000, 'Hân', '+84912345678', '12 Lý Thường Kiệt', '00004',
    @source, @carrier, @carrier_name, @code, @fee, @by, @reason, '2026-10-31T09:00:00Z', @shipped_at, @closed_at)`} {
			if _, err := db.Exec(ctx, q, a); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
	args := pgx.NamedArgs{"deal": deals["delivered"], "buyer": buyer}
	for _, q := range []string{
		`INSERT INTO deal_moves (id, deal_id, kind, by_party, moved_at)
		 VALUES (gen_random_uuid(), @deal, 'delivered', 'seller', now()), (gen_random_uuid(), @deal, 'returned', 'seller', now())`,
		`INSERT INTO notifications (id, account_id, notice_type, params, event_id, created_at, bumped_at)
		 VALUES (gen_random_uuid(), @buyer, 'delivered', '{"title": "Xe đạp", "seller": "Nam"}', gen_random_uuid(), now(), now()),
		        (gen_random_uuid(), @buyer, 'returned', '{"title": "Xe đạp"}', gen_random_uuid(), now(), now()),
		        (gen_random_uuid(), @buyer, 'order_cancelled', '{"reason": "sent_to_another"}', gen_random_uuid(), now(), now())`,
	} {
		if _, err := db.Exec(ctx, q, args); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("0040 on a database with orders: %v", err)
	}

	type order struct {
		status                         string
		mode, carrier, code, ship, why *string
		fee                            *int64
		deliveredAt, closedAt          *time.Time
	}
	read := func(name string) order {
		var o order
		if err := db.QueryRow(ctx, `SELECT status, shipment_mode, carrier, tracking_code, shipment_status, cancel_reason, fee,
       delivered_at, closed_at FROM orders WHERE id = @id`, pgx.NamedArgs{"id": orders[name]}).
			Scan(&o.status, &o.mode, &o.carrier, &o.code, &o.ship, &o.why, &o.fee, &o.deliveredAt, &o.closedAt); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return o
	}
	for name, want := range map[string]struct{ status, mode, carrier, ship, why string }{
		"requested": {"pending", "", "", "", ""},
		"shipped":   {"shipped", "manual", "spx", "in_transit", ""},
		"other":     {"shipped", "", "", "", ""},
		"delivered": {"completed", "manual", "ghn", "delivered", ""},
		"returned":  {"failed", "manual", "jt", "failed", ""},
		"cancelled": {"cancelled", "", "", "", "shipped_to_another"},
	} {
		o := read(name)
		if o.status != want.status || str(o.mode) != want.mode || str(o.carrier) != want.carrier || str(o.ship) != want.ship ||
			str(o.why) != want.why {
			t.Errorf("%s: %s %s %s %s %s", name, o.status, str(o.mode), str(o.carrier), str(o.ship), str(o.why))
		}
	}
	if o := read("shipped"); str(o.code) != "CODE-shipped" || o.fee == nil || *o.fee != 22_000 || o.deliveredAt != nil {
		t.Errorf("shipped: %s %v %v", str(o.code), o.fee, o.deliveredAt)
	}
	if o := read("delivered"); o.deliveredAt == nil || !o.deliveredAt.Equal(*o.closedAt) {
		t.Errorf("delivered at %v, closed at %v", o.deliveredAt, o.closedAt)
	}
	if o := read("other"); o.code != nil || o.fee != nil {
		t.Errorf("carrier other kept %s %v", str(o.code), o.fee)
	}

	var kinds []string
	if err := db.QueryRow(ctx, `SELECT array_agg(kind ORDER BY kind) FROM deal_moves WHERE deal_id = @deal`, args).
		Scan(&kinds); err != nil || !slices.Equal(kinds, []string{"completed", "failed"}) {
		t.Errorf("moves %v: %v", kinds, err)
	}
	var notices []string
	if err := db.QueryRow(ctx, `SELECT array_agg(notice_type || ':' || coalesce(params ->> 'reason', '') ORDER BY notice_type)
FROM notifications WHERE account_id = @buyer`, args).Scan(&notices); err != nil ||
		!slices.Equal(notices, []string{"completed:", "failed:", "order_cancelled:shipped_to_another"}) {
		t.Errorf("notices %v: %v", notices, err)
	}
	var gone int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
WHERE table_name = 'orders' AND column_name IN ('waybill_source', 'carrier_name')`).Scan(&gone); err != nil || gone != 0 {
		t.Errorf("%d old columns: %v", gone, err)
	}
}

// The checks keep the old names and a half shipment out, and a deal has
// one open order.
func TestOrderFlowChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	a := pgx.NamedArgs{"deal": uuid.NewV7(), "listing": catalogtest.Listing(t, db, seller), "buyer": buyer, "seller": seller}
	if _, err := db.Exec(ctx, insertDeal, a); err != nil {
		t.Fatal(err)
	}
	insert := func(status, mode, carrier, ship, reason string, delivered bool) error {
		b := pgx.NamedArgs{
			"status": status, "mode": null(mode), "carrier": null(carrier), "code": nil, "ship": null(ship),
			"delivered_at": nil, "by": nil, "reason": null(reason),
		}
		maps.Copy(b, a)
		if carrier != "" {
			b["code"] = "CODE1"
		}
		if delivered {
			b["delivered_at"] = time.Now()
		}
		if reason != "" {
			b["by"] = "system"
		}
		_, err := db.Exec(ctx, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name,
    recipient_phone, recipient_street, recipient_commune, shipment_mode, carrier, tracking_code, shipment_status, delivered_at,
    cancelled_by, cancel_reason, requested_at)
VALUES (gen_random_uuid(), @deal, @listing, @buyer, @seller, @status, 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004',
    @mode, @carrier, @code, @ship, @delivered_at, @by, @reason, now())`, b)
		return err
	}
	if err := insert("cancelled", "linked", "ghtk", "pending_pickup", "pickup_timeout", false); err != nil {
		t.Fatalf("a cancelled linked shipment: %v", err)
	}
	if err := insert("failed", "manual", "ghn", "failed", "", false); err != nil {
		t.Fatalf("a failed shipment: %v", err)
	}
	for name, err := range map[string]error{
		"status requested":         insert("requested", "", "", "", "", false),
		"carrier other":            insert("cancelled", "manual", "other", "in_transit", "buyer", false),
		"a carrier without a mode": insert("cancelled", "", "ghn", "in_transit", "buyer", false),
		"a mode without a status":  insert("cancelled", "manual", "ghn", "", "buyer", false),
		"delivered_at in transit":  insert("cancelled", "manual", "ghn", "in_transit", "buyer", true),
		"reason sent_to_another":   insert("cancelled", "", "", "", "sent_to_another", false),
	} {
		if err == nil {
			t.Errorf("%s was stored", name)
		}
	}
	if err := insert("pending", "", "", "", "", false); err != nil {
		t.Fatal(err)
	}
	if err := insert("shipped", "manual", "spx", "in_transit", "", false); err == nil {
		t.Fatal("a deal took a second open order")
	}
}
