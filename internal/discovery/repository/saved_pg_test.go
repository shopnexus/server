package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// listings inserts n listings, 20 per new seller (the cap), and returns
// their IDs.
func listings(t *testing.T, db *pgxpool.Pool, n int) []uuid.UUID {
	t.Helper()
	out := make([]uuid.UUID, 0, n)
	var seller uuid.UUID
	for i := range n {
		if i%catalogdomain.MaxOnSale == 0 {
			seller = accounttest.New(t, db)
		}
		out = append(out, catalogtest.Listing(t, db, seller))
	}
	return out
}

func TestSaves(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	ls := listings(t, db, 3)
	for range 2 {
		if err := s.Save(ctx, uuid.NewV7(), me, ls[0], 1_500_000, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(ctx, uuid.NewV7(), me, ls[1], 1_400_000, now); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, me, page.Request{})
	if err != nil || len(got) != 2 || got[0].ListingID != ls[1] || got[0].Price != 1_400_000 {
		t.Fatalf("%+v %v", got, err)
	}
	if err = s.Remove(ctx, me, ls[0]); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.List(ctx, me, page.Request{}); len(got) != 1 {
		t.Fatalf("after remove: %+v", got)
	}
}

func TestSaveLimit(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	ls := listings(t, db, domain.MaxSaved+1)
	for _, l := range ls[:domain.MaxSaved] {
		if err := s.Save(ctx, uuid.NewV7(), me, l, 1, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(ctx, uuid.NewV7(), me, ls[domain.MaxSaved], 1, now); !errors.Is(err, domain.ErrLimit) {
		t.Fatalf("the 501st: %v", err)
	}
	if err := s.Save(ctx, uuid.NewV7(), me, ls[0], 1, now); err != nil {
		t.Fatalf("saving one of the 500 again: %v", err)
	}
}

func TestSavedAmong(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	me, other := accounttest.New(t, db), accounttest.New(t, db)
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
	if got, err = s.SavedAmong(ctx, other, []uuid.UUID{a, b}); err != nil || len(got) != 0 {
		t.Fatalf("someone else's: %v %v", got, err)
	}
}

// PriceMoved runs PriceStep over the same prices: level 5 tr tells at 4.8,
// 4.5 and 4.9, and a repeated price writes no second event.
func TestPriceMoved(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	l := listings(t, db, 1)[0]
	if err := s.Save(ctx, uuid.NewV7(), me, l, 6_000_000, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, uuid.NewV7(), other, l, 6_000_000, now); err != nil { // the switch alone, at the saved price
		t.Fatal(err)
	}
	if found, err := s.SetAlert(ctx, me, l, new(int64(5_000_000))); err != nil || !found {
		t.Fatalf("%v %v", found, err)
	}
	var told []int
	for _, price := range []int64{6_000_000, 4_800_000, 4_800_000, 4_500_000, 5_500_000, 4_900_000} {
		n, err := s.PriceMoved(ctx, l, price, now)
		if err != nil {
			t.Fatal(err)
		}
		told = append(told, n)
	}
	// me: -, 4.8, -, 4.5, reset, 4.9; other (level 6 tr): -, 4.8, -, 4.5, -, -.
	if want := []int{0, 2, 0, 2, 0, 1}; !slices.Equal(told, want) {
		t.Fatalf("told %v, want %v", told, want)
	}
	var events int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'saved.price_dropped'`).Scan(&events); err != nil || events != 5 {
		t.Fatalf("%d events %v", events, err)
	}
	set := repository.NewSettings(db)
	if on, err := set.PriceCutAlerts(ctx, other); err != nil || !on {
		t.Fatalf("on by default: %v %v", on, err)
	}
	if err := set.SetPriceCutAlerts(ctx, other, false); err != nil {
		t.Fatal(err)
	}
	if n, err := s.PriceMoved(ctx, l, 4_000_000, now); err != nil || n != 1 { // me alone now
		t.Fatalf("switch off: %d %v", n, err)
	}
	got, err := s.List(ctx, me, page.Request{})
	if err != nil || got[0].AlertBelow == nil || *got[0].AlertBelow != 5_000_000 || got[0].Account != me {
		t.Fatalf("%+v %v", got, err)
	}
	one, found, err := s.Get(ctx, got[0].ID)
	if err != nil || !found || one.Account != me || one.ListingID != l {
		t.Fatalf("%+v %v %v", one, found, err)
	}
	if found, err = s.SetAlert(ctx, me, l, nil); err != nil || !found {
		t.Fatal(err)
	}
	if found, _ = s.SetAlert(ctx, me, uuid.NewV7(), nil); found {
		t.Fatal("no save: not found")
	}
}
