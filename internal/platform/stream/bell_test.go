package stream_test

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/stream"
)

var discard = slog.New(slog.DiscardHandler)

// rung is a ring a listener heard.
type rung struct {
	targets []stream.Target
	hint    stream.Hint
}

// listening runs a PGBell's Listen until the test ends and waits for it to
// listen. It returns the rings it hears and a signal per resync after the
// first.
func listening(t *testing.T, db *pgxpool.Pool) (rings <-chan rung, resyncs <-chan struct{}) {
	t.Helper()
	got := make(chan rung, 16)
	again := make(chan struct{}, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		stream.PGBell{DB: db, Log: discard}.Listen(ctx,
			func(targets []stream.Target, h stream.Hint) { got <- rung{targets, h} },
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

// heard waits for a ring of want and returns its targets.
func heard(t *testing.T, rings <-chan rung, want stream.Hint) []stream.Target {
	t.Helper()
	select {
	case got := <-rings:
		if got.hint.Kind != want.Kind || *got.hint.DealID != *want.DealID {
			t.Fatalf("heard %+v, want %+v", got.hint, want)
		}
		return got.targets
	case <-time.After(5 * time.Second):
		t.Fatal("no ring within 5s")
		return nil
	}
}

func silent(t *testing.T, rings <-chan rung) {
	t.Helper()
	select {
	case got := <-rings:
		t.Fatalf("an unexpected ring: %+v", got)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestNotifyRingsOnCommitOnly(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	rings, _ := listening(t, db)
	ctx := context.Background()
	a, b := uuid.NewV7(), uuid.NewV7()

	notify(t, db, []uuid.UUID{a}, dealHint(), false)
	silent(t, rings)

	committed := dealHint()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = queue(ctx, tx, []uuid.UUID{b, a, b}, committed); err != nil {
		t.Fatal(err)
	}
	silent(t, rings) // not before the commit
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	targets := heard(t, rings, committed)
	slices.SortFunc(targets, func(x, y stream.Target) int { return x.Account.Compare(y.Account) })
	if want := []stream.Target{{Account: a, Seq: 1}, {Account: b, Seq: 1}}; !slices.Equal(targets, want) {
		t.Fatalf("targets %+v, want %+v", targets, want)
	}
}

func TestRingArrivesAtOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	rings, _ := listening(t, db)
	now := dealHint()
	if err := (stream.PGBell{DB: db, Log: discard}).Ring(context.Background(), []stream.Target{{Account: uuid.NewV7()}}, now); err != nil {
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
