package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// Postgres generates the ID; the stdlib [uuid.UUID] scans it and binds it back.
func TestScanUUIDv7(t *testing.T) {
	t.Parallel()
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

// A statement past the pool's timeout is cancelled by Postgres.
func TestStatementTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, pgtest.URL(t), postgres.StatementTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, "SELECT pg_sleep(1)")
	if e, ok := errors.AsType[*pgconn.PgError](err); !ok || e.Code != "57014" {
		t.Fatalf("a slow statement ran: %v", err)
	}
	if _, err = pool.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatalf("a quick one: %v", err)
	}
}
