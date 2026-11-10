package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func TestSellerLeftDeletesTheListings(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	active := catalogtest.Listing(t, e.db, seller)
	hidden := catalogtest.Listing(t, e.db, seller)
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	sold := catalogtest.Listing(t, e.db, seller)
	if _, err := e.catalog.MarkSold(ctx, seller, sold); err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewListing(uuid.NewV7(), seller, draft(uuid.NewV7()), domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.listings.Insert(ctx, p); err != nil {
		t.Fatal(err)
	}
	other := catalogtest.Listing(t, e.db, e.seller(t))

	for range 2 {
		if err = e.catalog.SellerLeft(ctx, seller); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[uuid.UUID]domain.Status{
		active: domain.StatusDeleted, hidden: domain.StatusDeleted, p.ID(): domain.StatusDeleted,
		sold: domain.StatusSold, other: domain.StatusActive,
	} {
		l, getErr := e.listings.Get(ctx, id)
		if getErr != nil || l.Status() != want {
			t.Errorf("%s: %s %v, want %s", id, l.Status(), getErr, want)
		}
	}
	kinds := pgtest.EventKinds(t, e.db, active)
	if len(kinds) == 0 || kinds[len(kinds)-1] != "listing.deleted" {
		t.Fatalf("events of the active listing: %v", kinds)
	}
}
