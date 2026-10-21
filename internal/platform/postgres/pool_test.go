package postgres_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// Postgres generates the ID; the stdlib uuid.UUID scans it and binds it back.
func TestScanUUIDv7(t *testing.T) {
	pool := pgtest.Empty(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE TABLE items (id uuid PRIMARY KEY DEFAULT uuidv7(), name text)"); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	var text string
	if err := pool.QueryRow(ctx, "INSERT INTO items (name) VALUES ('a') RETURNING id, id::text").Scan(&id, &text); err != nil {
		t.Fatal(err)
	}
	if id.String() != text {
		t.Fatalf("scanned %s, text %s", id, text)
	}
	var back string
	err := pool.QueryRow(ctx, "SELECT name FROM items WHERE id = @id", pgx.StrictNamedArgs{"id": id}).Scan(&back)
	if err != nil || back != "a" {
		t.Fatalf("lookup by uuid: %q %v", back, err)
	}
}
