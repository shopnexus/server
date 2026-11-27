package service_test

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/boost/repository"
	"github.com/shopnexus/server/internal/boost/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/media/mediatest"
	paymentdomain "github.com/shopnexus/server/internal/payment/domain"
	paymentrepo "github.com/shopnexus/server/internal/payment/repository"
	paymentsvc "github.com/shopnexus/server/internal/payment/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	providerpayment "github.com/shopnexus/server/internal/provider/payment"
)

// A running 3-day package ticks once a day: a tick moves the listing up
// while it is on sale and skips a day it is hidden, without adding one,
// and the tick at the end ends it with boost.ended. A tick of a time that
// is not the boost's next does nothing.
//
// UC-LST-03 A6.
func TestBoostTicks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	in, prefix := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, prefix)
	catalog := catalogtest.Service(db, accounts, media, in, prefix)
	start := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	clock := restatetest.NewClock(start)
	store := repository.NewStore(db)
	boosts := service.New(service.Deps{
		Store: store, Payments: paymentsvc.New(paymentsvc.Deps{Store: paymentrepo.NewStore(db)}), Catalog: catalog,
		Accounts: accounts, Ingress: in, Prefix: prefix, Now: clock.Now, Log: slog.New(slog.DiscardHandler),
	})
	timers := service.Timers{Boosts: boosts}
	client := restatetest.Start(t, restate.Reflect(timers))

	seller := accounttest.New(t, db)
	listing := catalogtest.Listing(t, db, seller)
	id := uuid.NewV7()
	p, err := paymentrepo.NewStore(db).Insert(ctx,
		paymentdomain.NewPayment(uuid.NewV7(), seller, paymentdomain.ProductBoost, id, 15_000, "fake", "https://pay.test", start))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := store.Insert(ctx, domain.NewBoost(id, listing, seller, uuid.NewV7(), domain.PackageOf(3, 15_000), p.ID(), start))
	if err != nil {
		t.Fatal(err)
	}
	b.Start(start)
	if err = store.Save(ctx, &b, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	run := func(at time.Time) domain.State {
		t.Helper()
		clock.Add(at.Sub(clock.Now()))
		if _, tickErr := ingress.Service[service.TickRef, restate.Void](client, timers.ServiceName(), "Tick").
			Request(ctx, service.TickRef{BoostID: id, At: at}); tickErr != nil {
			t.Fatal(tickErr)
		}
		got, getErr := store.Get(ctx, id)
		if getErr != nil {
			t.Fatal(getErr)
		}
		return got.State()
	}
	bumpedAt := func() *time.Time {
		t.Helper()
		l, getErr := catalogrepo.NewListings(db).Get(ctx, listing)
		if getErr != nil {
			t.Fatal(getErr)
		}
		return l.BumpedAt()
	}
	day := 24 * time.Hour

	// 11.
	st := run(start.Add(day))
	if st.Bumps != 1+1 || !st.NextTickAt.Equal(start.Add(2*day)) || at(bumpedAt()) != start.Add(day) {
		t.Fatalf("day 2: %+v %v", st, bumpedAt())
	}
	// A tick of another time does nothing.
	if st = run(start.Add(day)); st.Bumps != 2 {
		t.Fatalf("a tick again: %+v", st)
	}
	// A6: hidden on day 3, no move up and no day added.
	if _, err = catalog.Hide(ctx, seller, listing, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	if st = run(start.Add(2 * day)); st.Bumps != 2 || !st.NextTickAt.Equal(start.Add(3*day)) || at(bumpedAt()) != start.Add(day) {
		t.Fatalf("day 3 hidden: %+v %v", st, bumpedAt())
	}
	// 12.
	if st = run(start.Add(3 * day)); st.Status != domain.StatusEnded || st.NextTickAt != nil || st.EndedAt == nil {
		t.Fatalf("the end: %+v", st)
	}
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE subject_id = $1 AND kind = 'boost.ended'`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("boost.ended %d %v", n, err)
	}
}

// at reads a time that must be there.
func at(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// A listing is boostable when Buy would sell it a package: on sale, with
// no boost awaiting payment or running, for a seller who may write and has
// a phone, on a server that takes payments.
func TestBoostStandings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	in, prefix := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	media := mediatest.Service(db, objectstoretest.New(t), in, prefix)
	catalog := catalogtest.Service(db, accounts, media, in, prefix)
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	newBoosts := func(gateway providerpayment.Client) *service.Boosts {
		return service.New(service.Deps{
			Store: repository.NewStore(db), Payments: paymentsvc.New(paymentsvc.Deps{Store: paymentrepo.NewStore(db), Gateway: gateway}),
			Catalog: catalog, Accounts: accounts, Ingress: in, Prefix: prefix, Now: func() time.Time { return now },
			Log: slog.New(slog.DiscardHandler),
		})
	}
	boosts := newBoosts(providerpayment.Fake{PagesURL: "http://pay.test"})

	seller := accounttest.WithPhone(t, db)
	free, running := catalogtest.Listing(t, db, seller), catalogtest.Listing(t, db, seller)
	id := uuid.NewV7()
	p, err := paymentrepo.NewStore(db).Insert(ctx,
		paymentdomain.NewPayment(uuid.NewV7(), seller, paymentdomain.ProductBoost, id, 15_000, "fake", "https://pay.test", now))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = repository.NewStore(db).Insert(ctx, domain.NewBoost(id, running, seller, uuid.NewV7(), domain.PackageOf(3, 15_000), p.ID(), now)); err != nil {
		t.Fatal(err)
	}
	listings := map[uuid.UUID]catalogdomain.Status{
		free: catalogdomain.StatusActive, running: catalogdomain.StatusActive, uuid.NewV7(): catalogdomain.StatusHidden,
	}
	got, err := boosts.Standings(ctx, seller, listings)
	if err != nil {
		t.Fatal(err)
	}
	if !got[free].Boostable || got[free].Boost != nil {
		t.Fatalf("free %+v", got[free])
	}
	if got[running].Boostable || got[running].Boost == nil || got[running].Boost.ID != id {
		t.Fatalf("awaiting payment %+v", got[running])
	}
	for l, st := range got {
		if l != free && l != running && st.Boostable {
			t.Fatalf("hidden %+v", st)
		}
	}

	// The seller's and the server's checks of Buy.
	suspended := accounttest.WithPhone(t, db)
	accounttest.Suspend(t, accounts, suspended)
	cases := []struct {
		name   string
		boosts *service.Boosts
		seller uuid.UUID
	}{
		{"no phone", boosts, accounttest.New(t, db)},
		{"suspended", boosts, suspended},
		{"no payments", newBoosts(nil), seller},
	}
	for _, c := range cases {
		got, err = c.boosts.Standings(ctx, c.seller, map[uuid.UUID]catalogdomain.Status{free: catalogdomain.StatusActive})
		if err != nil || got[free].Boostable {
			t.Fatalf("%s: %+v %v", c.name, got[free], err)
		}
	}
}
