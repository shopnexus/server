package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/migrations"
)

// 0064 gives each listing already there the first code of its ID.
func TestListingCodesBackfill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0064_listing_codes.sql")); err != nil {
		t.Fatal(err)
	}
	seller := accounttest.New(t, db)
	a, b := listing(t, db, seller), listing(t, db, seller)
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var wrong int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM listings WHERE id = ANY(@ids) AND code IS DISTINCT FROM listing_code(id, 0)`,
		pgx.StrictNamedArgs{"ids": []any{a, b}}).Scan(&wrong); err != nil || wrong != 0 {
		t.Fatalf("listings without their first code: %d %v", wrong, err)
	}
}
