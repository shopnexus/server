package service_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/service"
)

// sweeps records the watches Schedule was called with.
type sweeps struct {
	mu  sync.Mutex
	ids []uuid.UUID
}

func (s *sweeps) Schedule(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = append(s.ids, id)
	return nil
}

func (e env) matched(t *testing.T, watch uuid.UUID) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE kind = 'search_watch.matched' AND subject_id = $1`, watch).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWatchAndSweep(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	e.model.answers["xe đạp"] = `{"understood":"Xe đạp","boosts":[{"attr":"phrase","values":["xe đạp"]}],"demotes":[]}`
	v, created, err := e.discovery.Watch(ctx, me, service.WatchInput{Query: " xe đạp ", PriceMax: 3_000_000, Near: catalogdomain.Near{Commune: "00004"}})
	if err != nil || !created || v.Query != "xe đạp" || v.Area == nil || v.Area.CommuneCode != "00004" || v.Area.Name == "" || !v.Alert {
		t.Fatalf("%+v %v %v", v, created, err)
	}
	if len(e.sweeps.ids) != 1 || e.sweeps.ids[0] != v.ID {
		t.Fatalf("a new watch schedules its sweep: %v", e.sweeps.ids)
	}
	bike := catalogtest.Listing(t, e.db, accounttest.New(t, e.db)) // "Xe đạp Asama", 1.5 tr
	motor := catalogtest.Listing(t, e.db, accounttest.New(t, e.db), func(d *catalogdomain.ListingInput, _ *catalogdomain.Refs) { d.Title = "Xe máy Wave" })
	dear := catalogtest.Listing(t, e.db, accounttest.New(t, e.db), func(d *catalogdomain.ListingInput, _ *catalogdomain.Refs) { d.Price = 5_000_000 })
	hidden := catalogtest.Listing(t, e.db, accounttest.New(t, e.db))
	if err = e.discovery.Hide(ctx, me, hidden); err != nil {
		t.Fatal(err)
	}
	for _, l := range []uuid.UUID{bike, motor, dear, hidden} {
		if err = e.discovery.ListingArrived(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	r, err := e.discovery.Sweep(ctx, v.ID)
	if err != nil || !r.Found || r.Full {
		t.Fatalf("%+v %v", r, err)
	}
	if r, err = e.discovery.Sweep(ctx, v.ID); err != nil || !r.Found { // nothing new arrived
		t.Fatal(err)
	}
	l, err := e.discovery.Watches(ctx, me)
	if err != nil || len(l.Items) != 1 || l.Items[0].NewCount != 1 || e.matched(t, v.ID) != 1 {
		t.Fatalf("the bicycle alone, once: %+v %v %d", l, err, e.matched(t, v.ID))
	}
	sv, err := e.discovery.Search(ctx, &me, service.SearchInput{Watch: v.ID})
	if err != nil || !slices.Contains(ids(sv), bike) || slices.Contains(ids(sv), dear) || sv.WatchID == nil || *sv.WatchID != v.ID {
		t.Fatalf("?watch=: %v %+v %v", ids(sv), sv.WatchID, err)
	}
	if l, _ = e.discovery.Watches(ctx, me); l.Items[0].NewCount != 0 {
		t.Fatalf("the first page sees the watch: %+v", l)
	}
	if err = e.discovery.SetWatchAlert(ctx, me, v.ID, false); err != nil {
		t.Fatal(err)
	}
	if err = e.discovery.ListingArrived(ctx, catalogtest.Listing(t, e.db, accounttest.New(t, e.db))); err != nil {
		t.Fatal(err)
	}
	if _, err = e.discovery.Sweep(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ = e.discovery.Watches(ctx, me); l.Items[0].NewCount != 1 || e.matched(t, v.ID) != 1 {
		t.Fatalf("muted counts and tells nothing: %+v", l)
	}
	f, found, err := e.discovery.WatchFacts(ctx, v.ID)
	if err != nil || !found || f.Account != me || f.Query != "xe đạp" {
		t.Fatalf("%+v %v %v", f, found, err)
	}
	if err = e.discovery.Unwatch(ctx, me, v.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ = e.discovery.Sweep(ctx, v.ID); r.Found {
		t.Fatal("a deleted watch ends its chain")
	}
	again, created, err := e.discovery.Watch(ctx, me, service.WatchInput{Query: "xe đạp"})
	if err != nil || !created || again.ID == v.ID {
		t.Fatalf("watching again after unwatch is a new watch: %+v %v %v", again, created, err)
	}
}

func TestSweepFullBatch(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(d *service.Deps) { d.SweepBatch = 2 })
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	v, _, err := e.discovery.Watch(ctx, me, service.WatchInput{Query: "xe đạp"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err = e.discovery.ListingArrived(ctx, catalogtest.Listing(t, e.db, accounttest.New(t, e.db))); err != nil {
			t.Fatal(err)
		}
	}
	if r, _ := e.discovery.Sweep(ctx, v.ID); !r.Full {
		t.Fatal("two of three: full")
	}
	if r, _ := e.discovery.Sweep(ctx, v.ID); r.Full {
		t.Fatal("the last one: not full")
	}
	if l, _ := e.discovery.Watches(ctx, me); l.Items[0].NewCount != 3 {
		t.Fatalf("%+v", l)
	}
}

func TestWatchRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	for name, c := range map[string]struct {
		in   service.WatchInput
		want error
	}{
		"price range":      {service.WatchInput{Query: "a", PriceMin: 2, PriceMax: 1}, domain.ErrInvalidPriceRange},
		"radius, no place": {service.WatchInput{Query: "a", RadiusKm: 5}, domain.ErrPositionRequired},
		"unknown commune":  {service.WatchInput{Query: "a", Near: catalogdomain.Near{Commune: "99999"}}, catalogdomain.ErrInvalidPosition},
		"abroad":           {service.WatchInput{Query: "a", Near: catalogdomain.Near{Lat: 48.85, Lng: 2.35}}, catalogdomain.ErrInvalidPosition},
		"lat without lng":  {service.WatchInput{Query: "a", Near: catalogdomain.Near{Lat: 21.03}}, catalogdomain.ErrInvalidPosition},
	} {
		if _, _, err := e.discovery.Watch(ctx, me, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	v, _, err := e.discovery.Watch(ctx, me, service.WatchInput{Query: "xe", Near: catalogdomain.Near{Lat: baDinh.Lat, Lng: baDinh.Lng}})
	if err != nil || v.Area == nil {
		t.Fatalf("a GPS point becomes its commune: %+v %v", v, err)
	}
	var stored string
	if err = e.db.QueryRow(ctx, `SELECT coalesce(commune_code, '') FROM search_watches WHERE id = $1`, v.ID).Scan(&stored); err != nil || stored != v.Area.CommuneCode {
		t.Fatalf("%q %v", stored, err)
	}
	other := accounttest.New(t, e.db)
	if err = e.discovery.SetWatchAlert(ctx, other, v.ID, false); !errors.Is(err, domain.ErrWatchNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if _, err = e.discovery.Search(ctx, &other, service.SearchInput{Watch: v.ID}); !errors.Is(err, domain.ErrWatchNotFound) {
		t.Fatalf("?watch= of someone else's: %v", err)
	}
	if _, err = e.discovery.Search(ctx, &me, service.SearchInput{Watch: v.ID, Query: "x"}); !errors.Is(err, domain.ErrSearchConflict) {
		t.Fatalf("?watch= with q: %v", err)
	}
}
