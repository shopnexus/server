package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
)

func TestQueryVectors(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	c := repository.NewQueryVectors(db)
	at := time.Now()
	v := []float32{0.5, 0.5}
	if err := c.Put(ctx, "xe dap", 2, "m1", v, at, at.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Get(ctx, "xe dap", 2, "m1", at.Add(-time.Minute))
	if err != nil || !ok || !slices.Equal(got, v) {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	for name, args := range map[string]struct {
		model string
		ver   int
		since time.Time
	}{"model": {"m2", 2, at.Add(-time.Minute)}, "version": {"m1", 1, at.Add(-time.Minute)}, "stale": {"m1", 2, at.Add(time.Minute)}} {
		if _, found, _ := c.Get(ctx, "xe dap", args.ver, args.model, args.since); found {
			t.Errorf("%s: found", name)
		}
	}
	if err = c.Put(ctx, "noi com", 2, "m1", v, at.Add(time.Hour), at.Add(time.Minute)); err != nil {
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
	at := time.Now()
	up := uuid.NewV7()
	axis := embedtest.Axis(catalogdomain.PhotoDims, 0)
	if err := c.Put(ctx, up, "p1", axis, at.Add(time.Hour), at); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := c.Get(ctx, up, "p1", at); err != nil || !ok || got[0] != 1 {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	if _, ok, _ := c.Get(ctx, up, "p2", at); ok {
		t.Fatal("another model")
	}
	if _, ok, _ := c.Get(ctx, up, "p1", at.Add(2*time.Hour)); ok {
		t.Fatal("past its expiry")
	}
	if err := c.Put(ctx, uuid.NewV7(), "p1", axis, at.Add(3*time.Hour), at.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM search_photo_vectors`).Scan(&n)
	if n != 1 {
		t.Fatalf("the expired row stays: %d rows", n)
	}
}
