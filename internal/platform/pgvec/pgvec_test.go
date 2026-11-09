package pgvec_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/pgvec"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	v := []float32{0.5, -0.25, 1, 0}
	got, err := pgvec.Parse(pgvec.Format(v))
	if err != nil || !slices.Equal(got, v) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestParseRefuses(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "1,2", "[1,x]", "[1,,2]"} {
		if _, err := pgvec.Parse(s); err == nil {
			t.Errorf("Parse(%q) took it", s)
		}
	}
}

// TestHalfvecAcceptsTheForm checks the form against Postgres: a halfvec cast
// from Format prints back what Parse reads.
func TestHalfvecAcceptsTheForm(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	var out string
	err := db.QueryRow(context.Background(), `SELECT (@v::halfvec(3))::text`,
		pgx.StrictNamedArgs{"v": pgvec.Format([]float32{0.5, -0.25, 1})}).Scan(&out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgvec.Parse(out)
	if err != nil || !slices.Equal(got, []float32{0.5, -0.25, 1}) {
		t.Fatalf("%q → %v %v", out, got, err)
	}
}

func TestTablesExist(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	for _, table := range []string{"listing_text_vectors", "listing_photo_vectors", "search_photo_vectors", "search_query_vectors"} {
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			t.Errorf("%s: %v", table, err)
		}
	}
}
