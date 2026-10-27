package repository_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestUnderstandings(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewUnderstandings(db)
	now := time.Now()
	week := 7 * 24 * time.Hour
	if err := s.Put(ctx, "xe đạp", 1, json.RawMessage(`{"a":1}`), now, now.Add(-week)); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Get(ctx, "xe đạp", 1, now.Add(-time.Hour)); err != nil || !ok || string(got) != `{"a": 1}` {
		t.Fatalf("fresh: %s %v %v", got, ok, err)
	}
	if _, ok, _ := s.Get(ctx, "xe đạp", 1, now.Add(time.Hour)); ok {
		t.Fatal("an answer older than since was read")
	}
	if _, ok, _ := s.Get(ctx, "xe đạp", 2, now.Add(-time.Hour)); ok {
		t.Fatal("another prompt version's answer was read")
	}
	if err := s.Put(ctx, "cũ", 1, json.RawMessage(`{}`), now.Add(-8*24*time.Hour), now.Add(-9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "xe đạp", 1, json.RawMessage(`{"a":2}`), now, now.Add(-week)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM search_understandings`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the expired row stays: %d %v", n, err)
	}
	if got, _, _ := s.Get(ctx, "xe đạp", 1, now.Add(-time.Hour)); string(got) != `{"a": 2}` {
		t.Fatalf("a second put replaces: %s", got)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	key := uuid.NewV7().String()
	for range 8 {
		wg.Go(func() { errs <- s.Put(ctx, key, 1, json.RawMessage(`{}`), now, now.Add(-week)) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent put: %v", err)
		}
	}
}

func TestSavedAmong(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	me := newAccount(t, db)
	ids := listings(t, db, 2)
	a, b := ids[0], ids[1]
	s := repository.NewSaves(db)
	if err := s.Save(ctx, uuid.NewV7(), me, a, 100_000, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.SavedAmong(ctx, me, []uuid.UUID{a, b})
	if err != nil || !got[a] || got[b] {
		t.Fatalf("%v %v", got, err)
	}
}
