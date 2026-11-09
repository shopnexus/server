package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

func TestQueryVectors(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	c := repository.NewQueryVectors(db)
	now := time.Now()
	v := []float32{0.5, 0.5}
	if err := c.Put(ctx, "xe dap", 2, "m1", v, now, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Get(ctx, "xe dap", 2, "m1", now.Add(-time.Minute))
	if err != nil || !ok || !slices.Equal(got, v) {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	for name, args := range map[string]struct {
		model string
		ver   int
		since time.Time
	}{"model": {"m2", 2, now.Add(-time.Minute)}, "version": {"m1", 1, now.Add(-time.Minute)}, "stale": {"m1", 2, now.Add(time.Minute)}} {
		if _, found, _ := c.Get(ctx, "xe dap", args.ver, args.model, args.since); found {
			t.Errorf("%s: found", name)
		}
	}
	if err = c.Put(ctx, "noi com", 2, "m1", v, now.Add(time.Hour), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ = c.Get(ctx, "xe dap", 2, "m1", time.Time{}); ok {
		t.Fatal("the write of a new row deletes the expired ones")
	}
}

func TestPhotoVectors(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	c := repository.NewPhotoVectors(db)
	now := time.Now()
	up := uuid.NewV7()
	axis := embedtest.Axis(catalogdomain.PhotoDims, 0)
	if err := c.Put(ctx, up, "p1", axis, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := c.Get(ctx, up, "p1", now); err != nil || !ok || got[0] != 1 {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	if _, ok, _ := c.Get(ctx, up, "p2", now); ok {
		t.Fatal("another model")
	}
	if _, ok, _ := c.Get(ctx, up, "p1", now.Add(2*time.Hour)); ok {
		t.Fatal("past its expiry")
	}
	if err := c.Put(ctx, uuid.NewV7(), "p1", axis, now.Add(3*time.Hour), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM search_photo_vectors`).Scan(&n)
	if n != 1 {
		t.Fatalf("the expired row stays: %d rows", n)
	}
}
