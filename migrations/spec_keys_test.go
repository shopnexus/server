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

// 0081 keys each listing's spec whose label names a spec of its category,
// case aside, once per key; a label of the seller's own stays free.
func TestSpecKeysBackfill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0081_spec_keys.sql")); err != nil {
		t.Fatal(err)
	}
	seller := accounttest.New(t, db)
	insert := func(category, specs string) uuid.UUID {
		id := uuid.NewV7()
		if _, err := db.Exec(ctx, `INSERT INTO listings (id, seller_id, title, price, description, category_code, condition,
    allow_offers, commune_code, photos, specs, status, write_id, created_at, updated_at, published_at, lat, lng, code)
SELECT @id, @seller, 'Món hàng', 1500000, '', @category, 'used', true, c.code, '[]', @specs::jsonb, 'active', @id,
       @at, @at, @at, c.lat, c.lng, listing_code(@id, 0)
FROM communes c WHERE c.code = '00004'`,
			pgx.StrictNamedArgs{"id": id, "seller": seller, "category": category, "specs": specs, "at": time.Now()}); err != nil {
			t.Fatalf("insert listing: %v", err)
		}
		return id
	}
	bicycle := insert("vehicles.bicycle", `[{"label": "hãng", "value": "Asama"}, {"label": "Phanh", "value": "Đĩa"},
 {"label": "LOẠI XE", "value": "Xe đạp phố"}, {"label": "Hãng", "value": "Giant"}]`)
	monitor := insert("laptop.monitor", `[{"label": "Kích thước", "value": "27 inch"}, {"label": "Màu", "value": "Đen"}]`)
	none := insert("other.other", `[]`)
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uuid.UUID]string{
		bicycle: "brand=hãng,-=Phanh,vehicle_type=LOẠI XE,-=Hãng",
		monitor: "screen_size=Kích thước,-=Màu",
		none:    "",
	} {
		var got string
		if err := db.QueryRow(ctx, `SELECT coalesce(string_agg(coalesce(e ->> 'key', '-') || '=' || (e ->> 'label'), ',' ORDER BY i), '')
FROM listings LEFT JOIN LATERAL jsonb_array_elements(specs) WITH ORDINALITY AS s (e, i) ON true WHERE id = @id`,
			pgx.StrictNamedArgs{"id": id}).Scan(&got); err != nil || got != want {
			t.Errorf("%s: %q, want %q (%v)", id, got, want, err)
		}
	}
	// Search reads labels and values, not keys.
	var text string
	if err := db.QueryRow(ctx, `SELECT specs_text FROM listings WHERE id = @id`, pgx.StrictNamedArgs{"id": monitor}).Scan(&text); err != nil ||
		text != `["Kích thước", "Màu"] ["27 inch", "Đen"]` {
		t.Errorf("specs_text %q, %v", text, err)
	}
}
