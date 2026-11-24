package migrations_test

import (
	"context"
	"maps"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

const insertCarrierAccount = `INSERT INTO carrier_accounts (account_id, carrier, shop_code, token, nonce, linked_at)
VALUES (@account, @carrier, 'S19159', '\x01', '\x02', now())`

func TestCarrierAccounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	seller := accounttest.New(t, db)
	for _, c := range []string{"ghtk", "ghn"} {
		if _, err := db.Exec(ctx, insertCarrierAccount, pgx.NamedArgs{"account": seller, "carrier": c}); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
	}
	for name, c := range map[string]string{"a carrier no seller links": "viettel_post", "a second GHTK account": "ghtk"} {
		if _, err := db.Exec(ctx, insertCarrierAccount, pgx.NamedArgs{"account": seller, "carrier": c}); err == nil {
			t.Errorf("%s was stored", name)
		}
	}
	// A shop joined by a code keeps no token, and only at GHN.
	other := accounttest.New(t, db)
	const tokenless = `INSERT INTO carrier_accounts (account_id, carrier, shop_code, linked_at)
VALUES (@account, @carrier, '241109', now())`
	if _, err := db.Exec(ctx, tokenless, pgx.NamedArgs{"account": other, "carrier": "ghn"}); err != nil {
		t.Fatalf("a GHN shop without a token: %v", err)
	}
	if _, err := db.Exec(ctx, tokenless, pgx.NamedArgs{"account": other, "carrier": "ghtk"}); err == nil {
		t.Error("a GHTK account without a token was stored")
	}
}

// A linked shipment keeps a carrier's pickup point or a copy of an
// address, not both, and a manual one keeps neither.
func TestOrderPickups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	insert := func(mode, point, name string) error {
		a := pgx.NamedArgs{"deal": uuid.NewV7(), "listing": listing(t, db, seller), "buyer": buyer, "seller": seller}
		if _, err := db.Exec(ctx, insertPairDeal, a); err != nil {
			t.Fatal(err)
		}
		b := pgx.NamedArgs{"mode": mode, "point": null(point), "name": null(name), "phone": nil, "street": nil, "commune": nil, "shipped_at": time.Now()}
		if name != "" {
			b["phone"], b["street"], b["commune"] = "+84912345812", "12 Nguyễn Gia Trí", "00004"
		}
		maps.Copy(b, a)
		_, err := db.Exec(ctx, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name,
    recipient_phone, recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier, tracking_code, shipment_status,
    pickup_point_id, pickup_name, pickup_phone, pickup_street, pickup_commune, requested_at, shipped_at)
VALUES (gen_random_uuid(), @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    @mode, 'ghtk', 'S1.A1', 'pending_pickup', @point, @name, @phone, @street, @commune, now(), @shipped_at)`, b)
		return err
	}
	for name, err := range map[string]error{
		"a pickup point":    insert("linked", "88256", ""),
		"an address":        insert("linked", "", "Khánh Linh"),
		"the carrier's own": insert("linked", "", ""),
		"a manual shipment": insert("manual", "", ""),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, err := range map[string]error{
		"a point and an address":     insert("linked", "88256", "Khánh Linh"),
		"a manual shipment's pickup": insert("manual", "88256", ""),
	} {
		if err == nil {
			t.Errorf("%s was stored", name)
		}
	}
}
