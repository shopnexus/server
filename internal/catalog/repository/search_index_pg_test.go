package repository_test

import (
	"context"
	"testing"

	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// The search migration leaves one ParadeDB index on listings and the
// understanding cache.
func TestSearchIndex(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	var am string
	if err := db.QueryRow(ctx, `SELECT am.amname FROM pg_class c
JOIN pg_am am ON am.oid = c.relam WHERE c.relname = 'listings_search'`).Scan(&am); err != nil || (am != "paradedb" && am != "bm25") {
		t.Fatalf("index: %q %v", am, err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM search_understandings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}
