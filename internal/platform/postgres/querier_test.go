package postgres_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

func withTable(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := pgtest.Empty(t)
	if _, err := pool.Exec(context.Background(),
		"CREATE TABLE items (id uuid PRIMARY KEY DEFAULT uuidv7(), name text CONSTRAINT items_name_key UNIQUE)"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func count(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM items").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestInTxCommits(t *testing.T) {
	pool := withTable(t)
	err := postgres.NewTxRunner(pool).InTx(context.Background(), func(q postgres.Querier) error {
		_, err := q.Exec(context.Background(), "INSERT INTO items (name) VALUES ('a')")
		return err
	})
	if err != nil || count(t, pool) != 1 {
		t.Fatalf("err %v, count %d", err, count(t, pool))
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	pool := withTable(t)
	boom := errors.New("boom")
	err := postgres.NewTxRunner(pool).InTx(context.Background(), func(q postgres.Querier) error {
		if _, err := q.Exec(context.Background(), "INSERT INTO items (name) VALUES ('a')"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || count(t, pool) != 0 {
		t.Fatalf("err %v, count %d", err, count(t, pool))
	}
}

func TestInTxRollsBackOnPanic(t *testing.T) {
	pool := withTable(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic did not propagate")
			}
		}()
		postgres.NewTxRunner(pool).InTx(context.Background(), func(q postgres.Querier) error {
			q.Exec(context.Background(), "INSERT INTO items (name) VALUES ('a')")
			panic("boom")
		})
	}()
	if n := count(t, pool); n != 0 {
		t.Fatalf("count %d", n)
	}
}

func TestScanUUIDv7(t *testing.T) {
	pool := withTable(t)
	var id uuid.UUID
	var text string
	err := pool.QueryRow(context.Background(),
		"INSERT INTO items (name) VALUES ('a') RETURNING id, id::text").Scan(&id, &text)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != text {
		t.Fatalf("scanned %s, text %s", id, text)
	}
	var back string
	if err := pool.QueryRow(context.Background(), "SELECT name FROM items WHERE id = $1", id).Scan(&back); err != nil || back != "a" {
		t.Fatalf("lookup by uuid: %q %v", back, err)
	}
}

func TestUniqueViolation(t *testing.T) {
	pool := withTable(t)
	pool.Exec(context.Background(), "INSERT INTO items (name) VALUES ('a')")
	_, err := pool.Exec(context.Background(), "INSERT INTO items (name) VALUES ('a')")

	constraint, ok := postgres.UniqueViolation(err)
	if !ok || constraint != "items_name_key" {
		t.Fatalf("got %q %v (err %v)", constraint, ok, err)
	}
	if _, ok := postgres.UniqueViolation(errors.New("other")); ok {
		t.Fatal("plain error reported as unique violation")
	}
}
