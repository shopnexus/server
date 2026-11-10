package stream_test

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/stream"
)

var discard = slog.New(slog.DiscardHandler)

// listening runs a PGBell's Listen until the test ends and waits for it to
// listen. It returns the hints it hears and a signal per resync after the
// first.
func listening(t *testing.T, db *pgxpool.Pool) (rings <-chan stream.Hint, resyncs <-chan struct{}) {
	t.Helper()
	got := make(chan stream.Hint, 16)
	again := make(chan struct{}, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		stream.PGBell{DB: db, Log: discard}.Listen(ctx,
			func(_ []stream.Target, h stream.Hint) { got <- h },
			func() { again <- struct{}{} })
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	select {
	case <-again:
	case <-time.After(5 * time.Second):
		t.Fatal("not listening within 5s")
	}
	return got, again
}

func dealRing() stream.Hint {
	id := uuid.NewV7()
	return stream.Hint{Kind: stream.KindDeal, DealID: &id}
}

func heard(t *testing.T, rings <-chan stream.Hint, want stream.Hint) {
	t.Helper()
	select {
	case got := <-rings:
		if got.Kind != want.Kind || *got.DealID != *want.DealID {
			t.Fatalf("heard %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ring within 5s")
	}
}

func silent(t *testing.T, rings <-chan stream.Hint) {
	t.Helper()
	select {
	case got := <-rings:
		t.Fatalf("an unexpected ring: %+v", got)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestRingArrivesOnCommitOnly(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	rings, _ := listening(t, db)
	ctx := context.Background()
	bell := stream.PGBell{DB: db, Log: discard}
	targets := []stream.Target{{Account: uuid.NewV7()}}

	rolled := dealRing()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = bell.Ring(ctx, tx, targets, rolled); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	silent(t, rings)

	committed := dealRing()
	if tx, err = db.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if err = bell.Ring(ctx, tx, targets, committed); err != nil {
		t.Fatal(err)
	}
	silent(t, rings) // not before the commit
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	heard(t, rings, committed)

	now := dealRing()
	if err = bell.Ring(ctx, nil, targets, now); err != nil {
		t.Fatal(err)
	}
	heard(t, rings, now)
}

func TestListenResyncsAfterALostConnection(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	_, resyncs := listening(t, db)
	if _, err := db.Exec(context.Background(), `
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE datname = current_database() AND query = 'LISTEN stream'`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resyncs:
	case <-time.After(5 * time.Second):
		t.Fatal("no resync within 5s of a lost connection")
	}
}
