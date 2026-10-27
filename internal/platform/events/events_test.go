package events_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var at = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

func insert(t *testing.T, db *pgxpool.Pool, e events.Event) error {
	t.Helper()
	return pgx.BeginFunc(context.Background(), db, func(tx pgx.Tx) error { return events.Insert(context.Background(), tx, e) })
}

func TestInsertCommitsWithItsTransaction(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	e := events.Event{ID: uuid.NewV7(), Kind: events.AccountUpdated, SubjectID: uuid.NewV7(), OccurredAt: at}
	rollback := errors.New("roll back")
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := events.Insert(ctx, tx, e); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM events`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after rollback: %d %v", n, err)
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "LISTEN events"); err != nil {
		t.Fatal(err)
	}
	if err = insert(t, db, e); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err = conn.Conn().WaitForNotification(wait); err != nil {
		t.Fatalf("no notification after commit: %v", err)
	}
	var kind string
	if err = db.QueryRow(ctx, `SELECT kind FROM events WHERE id = @id AND dispatched_at IS NULL`,
		pgx.StrictNamedArgs{"id": e.ID}).Scan(&kind); err != nil || kind != "account.updated" {
		t.Fatalf("%q %v", kind, err)
	}
}

// A nil Data is stored as {} and arrives as an empty map.
func TestInsertStoresEmptyData(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	e := events.Event{ID: uuid.NewV7(), Kind: events.AccountSuspended, SubjectID: uuid.NewV7(), OccurredAt: at}
	if err := insert(t, db, e); err != nil {
		t.Fatal(err)
	}
	var data string
	if err := db.QueryRow(context.Background(), `SELECT payload::text FROM events`).Scan(&data); err != nil || data != "{}" {
		t.Fatalf("%q %v", data, err)
	}
}

// A durable step that retries its save writes the same event again; the
// second insert does nothing.
func TestInsertTwiceKeepsOne(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	e := events.Event{ID: uuid.NewV7(), Kind: events.AccountUpdated, SubjectID: uuid.NewV7(), OccurredAt: at}
	for range 2 {
		if err := insert(t, db, e); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM events`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
}
