package repository_test

import (
	"context"
	"crypto/sha256"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

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
	if err := c.Put(ctx, "m1", "xe dap", v, at); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Get(ctx, "m1", "  Xe  DAP ", at)
	if err != nil || !ok || !slices.Equal(got, v) {
		t.Fatalf("the normalized text finds the row: %v %v %v", got, ok, err)
	}
	if _, ok, _ = c.Get(ctx, "m2", "xe dap", at); ok {
		t.Fatal("another model's version found the row")
	}
	if hits := hitsOf(t, db, "xe dap"); hits != 0 {
		t.Fatalf("a read on the day of the write counted: %d", hits)
	}
	if _, _, err = c.Get(ctx, "m1", "xe dap", at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Get(ctx, "m1", "xe dap", at.Add(26*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if hits := hitsOf(t, db, "xe dap"); hits != 1 {
		t.Fatalf("two reads in a day count once: %d", hits)
	}
}

// hitsOf is the hits of the m1 row of text.
func hitsOf(t *testing.T, db *pgxpool.Pool, text string) int {
	t.Helper()
	var n int
	h := sha256.Sum256([]byte(text))
	if err := db.QueryRow(context.Background(), `SELECT hits FROM search_text_vectors
WHERE model_version = 'm1' AND text_hash = $1`, h[:]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestQueryVectorsSweep(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	c := repository.NewQueryVectors(db)
	at := time.Now()
	day := 24 * time.Hour
	v := []float32{1, 0}
	put := func(text string, age time.Duration) {
		t.Helper()
		if err := c.Put(ctx, "m1", text, v, at.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	put("unread fresh", 10*day)
	put("unread old", 40*day)
	put("read", 100*day)
	put("read long ago", 300*day)
	if _, _, err := c.Get(ctx, "m1", "read", at.Add(-50*day)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Get(ctx, "m1", "read long ago", at.Add(-200*day)); err != nil {
		t.Fatal(err)
	}
	n, err := c.Sweep(ctx, at, 30*day, 180*day, 10)
	if err != nil || n != 2 {
		t.Fatalf("deleted %d, want the unread old and the read long ago: %v", n, err)
	}
	for text, want := range map[string]bool{"unread fresh": true, "unread old": false, "read": true, "read long ago": false} {
		if _, ok, _ := c.Get(ctx, "m1", text, at); ok != want {
			t.Errorf("%s kept: %v", text, ok)
		}
	}
	// Past the cap the row read on fewer days goes first.
	if n, err = c.Sweep(ctx, at, 30*day, 180*day, 1); err != nil || n != 1 {
		t.Fatalf("cap: %d %v", n, err)
	}
	if _, ok, _ := c.Get(ctx, "m1", "read", at); !ok {
		t.Fatal("the cap deleted the row a search read")
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
