package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/boost/repository"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	paymentdomain "github.com/shopnexus/server/internal/payment/domain"
	paymentrepo "github.com/shopnexus/server/internal/payment/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// purchase is a boost of pkg for the listing with its payment stored.
func purchase(t *testing.T, db *pgxpool.Pool, listing, seller uuid.UUID, pkg domain.Package, at time.Time) domain.Boost {
	t.Helper()
	id := uuid.NewV7()
	p, err := paymentrepo.NewStore(db).Insert(context.Background(),
		paymentdomain.NewPayment(uuid.NewV7(), seller, paymentdomain.ProductBoost, id, pkg.Price, "fake", "https://pay.test", at))
	if err != nil {
		t.Fatal(err)
	}
	return domain.NewBoost(id, listing, seller, uuid.NewV7(), pkg, p.ID(), at)
}

func TestBoostInsertOneLivePerListing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	store := repository.NewStore(db)
	seller := accounttest.New(t, db)
	listing := catalogtest.Listing(t, db, seller)
	at := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	short, long := domain.PackageOf(3, 15_000), domain.PackageOf(7, 29_000)

	first := purchase(t, db, listing, seller, short, at)
	got, existed, err := store.Insert(ctx, first)
	if err != nil || existed || got.ID() != first.ID() {
		t.Fatalf("first %v %v %v", got.ID(), existed, err)
	}
	// The same key returns the boost it made.
	if got, existed, err = store.Insert(ctx, first); err != nil || !existed || got.ID() != first.ID() {
		t.Fatalf("same key %v %v %v", got.ID(), existed, err)
	}
	// Another purchase of the same package picks the awaiting one up.
	if got, existed, err = store.Insert(ctx, purchase(t, db, listing, seller, short, at)); err != nil || !existed || got.ID() != first.ID() {
		t.Fatalf("same package %v %v %v", got.ID(), existed, err)
	}
	// Another package cancels it.
	second := purchase(t, db, listing, seller, long, at.Add(time.Minute))
	if got, existed, err = store.Insert(ctx, second); err != nil || existed || got.ID() != second.ID() {
		t.Fatalf("other package %v %v %v", got.ID(), existed, err)
	}
	if old, _ := store.Get(ctx, first.ID()); old.Status() != domain.StatusCancelled || old.State().EndedAt == nil {
		t.Fatalf("replaced %+v", old.State())
	}
	// A running one refuses another purchase, naming itself.
	second.Start(at.Add(2 * time.Minute))
	if err = store.Save(ctx, &second, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Insert(ctx, purchase(t, db, listing, seller, short, at.Add(3*time.Minute)))
	if e := apperr.From(err); !errors.Is(err, domain.ErrActive) || e.Details.BoostID == nil || *e.Details.BoostID != second.ID() {
		t.Fatalf("while running: %v", err)
	}

	live, err := store.Live(ctx, []uuid.UUID{listing, uuid.NewV7()})
	running := live[listing]
	if err != nil || len(live) != 1 || running.ID() != second.ID() {
		t.Fatalf("live %v %v", live, err)
	}
	byPayment, err := store.ByPayment(ctx, second.PaymentID())
	if err != nil || byPayment.ID() != second.ID() {
		t.Fatalf("by payment %v %v", byPayment.ID(), err)
	}
	byKey, err := store.ByKey(ctx, seller, first.State().RequestKey)
	if err != nil || byKey.ID() != first.ID() {
		t.Fatalf("by key %v %v", byKey.ID(), err)
	}
	if _, err = store.Get(ctx, uuid.NewV7()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	all, err := store.OfListing(ctx, listing, page.Request{Limit: 1})
	if err != nil || len(all) != 2 || all[0].ID() != second.ID() {
		t.Fatalf("of listing %d %v", len(all), err)
	}
	stale := second
	second.Ticked(*second.NextTick(), at.Add(24*time.Hour), true)
	if err = store.Save(ctx, &second, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err = store.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
}
