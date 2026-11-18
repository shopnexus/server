package repository_test

import (
	"context"
	"slices"
	"testing"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestHidden(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	h := repository.NewHidden(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	ls := listings(t, db, 3)
	for _, l := range ls {
		if err := h.Hide(ctx, me, l); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Hide(ctx, me, ls[0]); err != nil { // again: moves to the newest
		t.Fatal(err)
	}
	got, err := h.List(ctx, me)
	if err != nil || len(got) != 3 || got[0] != ls[0] {
		t.Fatalf("%v %v", got, err)
	}
	if err = h.Unhide(ctx, me, ls[1]); err != nil {
		t.Fatal(err)
	}
	if err = h.Unhide(ctx, me, ls[1]); err != nil { // not hidden: fine
		t.Fatal(err)
	}
	if got, _ = h.List(ctx, me); slices.Contains(got, ls[1]) || len(got) != 2 {
		t.Fatalf("unhide: %v", got)
	}
	if err = h.Clear(ctx, me); err != nil {
		t.Fatal(err)
	}
	if got, _ = h.List(ctx, me); len(got) != 0 {
		t.Fatalf("clear: %v", got)
	}
}

// The 1,001st hide shows the oldest again.
func TestHiddenKeepsTheNewest(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	h := repository.NewHidden(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	ls := listings(t, db, domain.MaxHidden+1)
	for _, l := range ls {
		if err := h.Hide(ctx, me, l); err != nil {
			t.Fatal(err)
		}
	}
	got, err := h.List(ctx, me)
	if err != nil || len(got) != domain.MaxHidden || slices.Contains(got, ls[0]) || got[0] != ls[len(ls)-1] {
		t.Fatalf("%d %v", len(got), err)
	}
}
