package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// sold is the status whose saves List puts last, for the tests.
var sold = []catalogdomain.Status{catalogdomain.StatusSold}

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
	got, err := s.List(ctx, me, sold, page.Request{})
	if err != nil || len(got) != 2 || got[0].ListingID != ls[1] || got[0].Price != 1_400_000 {
		t.Fatalf("%+v %v", got, err)
	}
	if err = s.Remove(ctx, me, ls[0]); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.List(ctx, me, sold, page.Request{}); len(got) != 1 {
		t.Fatalf("after remove: %+v", got)
	}
}

// TestListPutsEndedLast pages through saves two at a time: the saves of
// listings in a last status follow the others, each newest first, and a
// cursor whose save was removed goes on after the saves on sale.
func TestListPutsEndedLast(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	me := accounttest.New(t, db)
	ls := listings(t, db, 5)
	saves := make([]uuid.UUID, len(ls))
	for i, l := range ls {
		saves[i] = uuid.NewV7()
		if err := s.Save(ctx, saves[i], me, l, 1_000_000, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE listings SET status = 'sold', sold_at = now() WHERE id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": []uuid.UUID{ls[1], ls[3]}}); err != nil {
		t.Fatal(err)
	}
	var got []uuid.UUID
	p := page.Request{Limit: 2}
	for {
		rows, err := s.List(ctx, me, sold, p)
		if err != nil {
			t.Fatal(err)
		}
		pg := page.Of(rows, p, func(r repository.Save) uuid.UUID { return r.ID })
		for _, r := range pg.Items {
			got = append(got, r.ID)
		}
		if pg.NextCursor == nil {
			break
		}
		p.Cursor = *pg.NextCursor
	}
	if want := []uuid.UUID{saves[4], saves[2], saves[0], saves[3], saves[1]}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := s.Remove(ctx, me, ls[0]); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(ctx, me, sold, page.Request{Cursor: saves[0], Limit: 2})
	if err != nil || len(rows) != 2 || rows[0].ID != saves[3] || rows[1].ID != saves[1] {
		t.Fatalf("after a removed cursor: %+v %v", rows, err)
	}
}

// UC-DSC-03 E4 · BR-DSC-03 · NFR-CONC-01.
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
	if err := s.Save(ctx, uuid.NewV7(), other, l, 6_000_000, now); err != nil { // no level of its own: the saved price
		t.Fatal(err)
	}
	if found, err := s.SetAlert(ctx, me, l, new(int64(5_000_000))); err != nil || !found {
		t.Fatalf("%v %v", found, err)
	}
	told := make([]int, 0, 6)
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
	if n, err := s.PriceMoved(ctx, l, 4_000_000, now); err != nil || n != 2 {
		t.Fatalf("a new low tells both: %d %v", n, err)
	}
	got, err := s.List(ctx, me, sold, page.Request{})
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

// Recent lists the saves made at or after a time, newest first, of one
// account only, up to a limit.
func TestSavesRecent(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSaves(db)
	ctx := context.Background()
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	ls := listings(t, db, 3)
	for _, save := range []struct {
		who     uuid.UUID
		listing uuid.UUID
		at      time.Time
	}{{me, ls[0], now.AddDate(0, 0, -40)}, {me, ls[1], now.AddDate(0, 0, -2)}, {me, ls[2], now}, {other, ls[0], now}} {
		if err := s.Save(ctx, uuid.NewV7(), save.who, save.listing, 1_500_000, save.at); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Recent(ctx, me, now.AddDate(0, 0, -29), 10)
	if err != nil || !slices.Equal(got, []uuid.UUID{ls[2], ls[1]}) {
		t.Fatalf("%v %v", got, err)
	}
	if got, err = s.Recent(ctx, me, now.AddDate(0, 0, -29), 1); err != nil || !slices.Equal(got, []uuid.UUID{ls[2]}) {
		t.Fatalf("a limit of 1: %v %v", got, err)
	}
}
