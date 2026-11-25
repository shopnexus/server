package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// 0096 holds a listing's status to its timestamps and order, its photos
// to 1–10 and its specs to 20, and an upload's status to its keys and
// holder; a released upload is stored.
func TestCatalogMediaChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	seller := accounttest.New(t, db)
	for name, set := range map[string]string{
		"reserved for no order":      `status = 'reserved'`,
		"an order on an active one":  `reserved_for = gen_random_uuid()`,
		"sold with no time":          `status = 'sold'`,
		"a sale time while on sale":  `sold_at = now()`,
		"deleted with no time":       `status = 'deleted'`,
		"a deletion time on sale":    `deleted_at = now()`,
		"no photo":                   `photos = '[]'`,
		"photos that are not a list": `photos = 'null'`,
		"11 photos":                  `photos = (SELECT jsonb_agg(photos -> 0) FROM generate_series(1, 11))`,
		"21 specs":                   `specs = (SELECT jsonb_agg(jsonb_build_object('label', 'a', 'value', 'b')) FROM generate_series(1, 21))`,
		"specs that are not a list":  `specs = '{}'`,
	} {
		id := listing(t, db, seller)
		if _, err := db.Exec(ctx, `UPDATE listings SET `+set+` WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err == nil {
			t.Errorf("%s was stored", name)
		}
	}
	id := listing(t, db, seller)
	if _, err := db.Exec(ctx, `UPDATE listings SET status = 'sold', sold_at = now() WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatalf("a sale: %v", err)
	}
	if _, err := db.Exec(ctx, `UPDATE listings SET status = 'deleted', deleted_at = now() WHERE id = @id`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatalf("a sold listing deleted, keeping sold_at: %v", err)
	}

	for name, c := range map[string]struct {
		status, holder, photo string
		ok                    bool
	}{
		"waiting":               {"waiting", "NULL", "NULL", true},
		"waiting with a holder": {"waiting", "gen_random_uuid()", "NULL", false},
		"ready without its key": {"ready", "gen_random_uuid()", "NULL", false},
		"ready":                 {"ready", "gen_random_uuid()", "'media/a.jpg'", true},
		"released":              {"released", "gen_random_uuid()", "'media/a.jpg'", true},
	} {
		shape := "NULL"
		if c.holder != "NULL" {
			shape = "'photo'"
		}
		_, err := db.Exec(ctx, `INSERT INTO uploads (id, owner_id, holder_id, shape, content_type, size, status, photo_key,
    write_id, created_at, updated_at)
VALUES (gen_random_uuid(), @owner, `+c.holder+`, `+shape+`, 'image/jpeg', 10, @status, `+c.photo+`, gen_random_uuid(), now(), now())`,
			pgx.StrictNamedArgs{"owner": seller, "status": c.status})
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}

	var left int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
WHERE (table_name, column_name) IN (('listing_reads', 'estimate'), ('account_updates', 'video_read_id'),
    ('account_updates', 'link_read_id')) OR table_name IN ('video_reads', 'link_reads')`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("legacy columns left: %d %v", left, err)
	}
}
