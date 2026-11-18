package repository_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestUnderstandings(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewUnderstandings(db)
	at := time.Now()
	week := 7 * 24 * time.Hour
	if err := s.Put(ctx, "xe đạp", 1, json.RawMessage(`{"a":1}`), at, at.Add(-week)); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Get(ctx, "xe đạp", 1, at.Add(-time.Hour)); err != nil || !ok || string(got) != `{"a": 1}` {
		t.Fatalf("fresh: %s %v %v", got, ok, err)
	}
	if _, ok, _ := s.Get(ctx, "xe đạp", 1, at.Add(time.Hour)); ok {
		t.Fatal("an answer older than since was read")
	}
	if _, ok, _ := s.Get(ctx, "xe đạp", 2, at.Add(-time.Hour)); ok {
		t.Fatal("another prompt version's answer was read")
	}
	if err := s.Put(ctx, "cũ", 1, json.RawMessage(`{}`), at.Add(-8*24*time.Hour), at.Add(-9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "xe đạp", 1, json.RawMessage(`{"a":2}`), at, at.Add(-week)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM search_understandings`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the expired row stays: %d %v", n, err)
	}
	if got, _, _ := s.Get(ctx, "xe đạp", 1, at.Add(-time.Hour)); string(got) != `{"a": 2}` {
		t.Fatalf("a second put replaces: %s", got)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	key := uuid.NewV7().String()
	for range 8 {
		wg.Go(func() { errs <- s.Put(ctx, key, 1, json.RawMessage(`{}`), at, at.Add(-week)) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent put: %v", err)
		}
	}
}
