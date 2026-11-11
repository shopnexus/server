package stream_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/stream"
)

func notify(t *testing.T, db *pgxpool.Pool, accounts []uuid.UUID, h stream.Hint, commit bool) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Notify(ctx, tx, accounts, h); err != nil {
		t.Fatal(err)
	}
	if commit {
		err = tx.Commit(ctx)
	} else {
		err = tx.Rollback(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func dealHint() stream.Hint {
	id := uuid.NewV7()
	return stream.Hint{Kind: stream.KindDeal, DealID: &id}
}

func seqs(p stream.Page) []int64 {
	out := make([]int64, 0, len(p.Updates))
	for _, u := range p.Updates {
		out = append(out, u.Seq)
	}
	return out
}

func since(t *testing.T, db *pgxpool.Pool, account uuid.UUID, after int64, limit int) stream.Page {
	t.Helper()
	p, err := stream.PGLog{DB: db}.Since(context.Background(), account, after, limit)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSeqRunsWithoutHoles(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	a := uuid.NewV7()
	first := dealHint()
	notify(t, db, []uuid.UUID{a}, first, true)
	notify(t, db, []uuid.UUID{a}, dealHint(), true)
	notify(t, db, []uuid.UUID{a}, dealHint(), false) // rolled back: no seq taken
	notify(t, db, []uuid.UUID{a}, dealHint(), true)
	p := since(t, db, a, 0, 10)
	if got := seqs(p); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 || p.Seq != 3 || p.Reset || p.HasMore {
		t.Fatalf("%v %+v", got, p)
	}
	if u := p.Updates[0]; u.Kind != stream.KindDeal || *u.DealID != *first.DealID {
		t.Fatalf("first update %+v", u)
	}
	cur, err := stream.PGLog{DB: db}.Current(context.Background(), a)
	if err != nil || cur != 3 {
		t.Fatalf("current %d %v", cur, err)
	}
	if cur, err = (stream.PGLog{DB: db}).Current(context.Background(), uuid.NewV7()); err != nil || cur != 0 {
		t.Fatalf("a new account's current %d %v", cur, err)
	}
}

// Two transactions notify the same two accounts in opposite orders, many
// times at once; Notify locks in UUID order, so neither deadlocks.
func TestNotifyDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	a, b := uuid.NewV7(), uuid.NewV7()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, pair := range [][]uuid.UUID{{a, b}, {b, a}} {
		wg.Go(func() {
			for range 30 {
				if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
					return stream.Notify(ctx, tx, pair, dealHint())
				}); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for _, acc := range []uuid.UUID{a, b} {
		if p := since(t, db, acc, 0, 100); len(p.Updates) != 60 || p.Seq != 60 {
			t.Fatalf("%s: %d updates, seq %d", acc, len(p.Updates), p.Seq)
		}
	}
}

// A transaction that takes seq 2 waits for the one holding seq 1, so a
// reader never sees 2 while 1 may still commit.
func TestSeqBecomesVisibleInCommitOrder(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	a := uuid.NewV7()
	first := dealHint()
	tx1, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Notify(ctx, tx1, []uuid.UUID{a}, first); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- pgx.BeginFunc(ctx, db, func(tx2 pgx.Tx) error {
			return stream.Notify(ctx, tx2, []uuid.UUID{a}, dealHint())
		})
	}()
	select {
	case err = <-done:
		t.Fatalf("the second notify finished before the first committed: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err = tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	p := since(t, db, a, 0, 10)
	if got := seqs(p); len(got) != 2 || *p.Updates[0].DealID != *first.DealID {
		t.Fatalf("%v %+v", got, p.Updates)
	}
}

func TestSincePages(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	a := uuid.NewV7()
	for range 5 {
		notify(t, db, []uuid.UUID{a}, dealHint(), true)
	}
	for _, c := range []struct {
		after int64
		want  []int64
		more  bool
	}{{0, []int64{1, 2}, true}, {2, []int64{3, 4}, true}, {4, []int64{5}, false}, {5, []int64{}, false}} {
		p := since(t, db, a, c.after, 2)
		got := seqs(p)
		if p.Reset || p.HasMore != c.more || p.Seq != 5 || len(got) != len(c.want) || p.Updates == nil {
			t.Fatalf("after %d: %v %+v", c.after, got, p)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("after %d: %v, want %v", c.after, got, c.want)
			}
		}
	}
}

func TestSinceResets(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	a := uuid.NewV7()
	for range 3 {
		notify(t, db, []uuid.UUID{a}, dealHint(), true)
	}
	if p := since(t, db, a, 9, 10); !p.Reset || p.Seq != 3 || p.Updates == nil || len(p.Updates) != 0 {
		t.Fatalf("ahead: %+v", p)
	}
	if _, err := db.Exec(ctx, `UPDATE account_updates SET created_at = now() - interval '31 days' WHERE account_id = @a AND seq = 1`,
		pgx.StrictNamedArgs{"a": a}); err != nil {
		t.Fatal(err)
	}
	n, err := stream.PGLog{DB: db}.Prune(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if p := since(t, db, a, 0, 10); !p.Reset || p.Seq != 3 {
		t.Fatalf("pruned past since: %+v", p)
	}
	if p := since(t, db, a, 1, 10); p.Reset || len(p.Updates) != 2 {
		t.Fatalf("after the pruned row: %+v", p)
	}
}

func TestListingReadUpdate(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	a, read := uuid.NewV7(), uuid.NewV7()
	notify(t, db, []uuid.UUID{a}, stream.Hint{Kind: stream.KindListingRead, ListingReadID: &read}, true)
	p := since(t, db, a, 0, 10)
	if len(p.Updates) != 1 {
		t.Fatalf("%+v", p)
	}
	if u := p.Updates[0]; u.Kind != "listing_read" || u.ListingReadID == nil || *u.ListingReadID != read || u.DealID != nil {
		t.Fatalf("update %+v", u)
	}
}
