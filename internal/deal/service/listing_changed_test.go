package service_test

import (
	"context"
	"testing"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
)

// changed calls ListingChanged for listing.
func (e env) changed(t *testing.T, listing uuid.UUID) {
	t.Helper()
	if err := e.deals.ListingChanged(context.Background(), listing); err != nil {
		t.Fatal(err)
	}
}

// count counts the moves of kind in v.
func count(v []domain.MoveKind, kind domain.MoveKind) int {
	n := 0
	for _, k := range v {
		if k == kind {
			n++
		}
	}
	return n
}

func TestListingChanged(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, a, b := e.person(t), e.person(t), e.person(t)
	listing := e.listing(t, seller)
	da, db := e.open(t, a, listing), e.open(t, b, listing)
	must(t)(e.deals.Offer(ctx, a, da.ID, 1_000_000))
	must(t)(e.deals.Accept(ctx, b, db.ID, 1_500_000))

	e.edit(t, listing, func(d *catalogdomain.Draft) { d.Price = 1_400_000 })
	e.changed(t, listing)
	e.changed(t, listing)
	va := e.get(t, a, da.ID)
	if va.OpenPrice != nil || count(moveKinds(va), domain.MovePriceChanged) != 1 {
		t.Fatalf("%+v", va)
	}
	if m := va.Moves[len(va.Moves)-1]; m.By != domain.System || m.Amount == nil || *m.Amount != 1_400_000 {
		t.Fatalf("%+v", m)
	}
	if vb := e.get(t, b, db.ID); vb.Status != domain.StatusAgreed {
		t.Fatalf("%+v", vb)
	}

	must(t)(e.deals.Offer(ctx, a, da.ID, 900_000))
	e.changed(t, listing)
	if va = e.get(t, a, da.ID); va.OpenPrice == nil || va.OpenPrice.Amount != 900_000 {
		t.Fatalf("a late event took the new offer: %+v", va)
	}

	before := len(kindsOf(t, e.db, db.ID))
	if _, err := e.catalog.MarkSold(ctx, seller, listing); err != nil {
		t.Fatal(err)
	}
	e.changed(t, listing)
	if va = e.get(t, a, da.ID); va.OpenPrice != nil || moveKinds(va)[len(va.Moves)-1] != domain.MoveListingClosed {
		t.Fatalf("%+v", va)
	}
	vb := e.get(t, b, db.ID)
	if m := vb.Moves[len(vb.Moves)-1]; vb.Status != domain.StatusNegotiating || m.Kind != domain.MoveListingClosed ||
		m.By != domain.System {
		t.Fatalf("%+v", vb)
	}
	if after := len(kindsOf(t, e.db, db.ID)); after != before+1 {
		t.Fatalf("events %d → %d", before, after)
	}
}

func TestListingChangedOffersOff(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	seller, buyer := e.person(t), e.person(t)
	listing := e.listing(t, seller)
	v := e.open(t, buyer, listing)
	must(t)(e.deals.Offer(context.Background(), buyer, v.ID, 1_000_000))
	e.edit(t, listing, func(d *catalogdomain.Draft) { d.AllowOffers = false })
	e.changed(t, listing)
	if v = e.get(t, buyer, v.ID); v.OpenPrice != nil || moveKinds(v)[len(v.Moves)-1] != domain.MoveOffersClosed {
		t.Fatalf("%+v", v)
	}
}

func TestListingChangedHidden(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	listing := e.listing(t, seller)
	v := e.open(t, buyer, listing)
	must(t)(e.deals.Offer(ctx, buyer, v.ID, 1_000_000))
	if _, err := e.catalog.Hide(ctx, seller, listing); err != nil {
		t.Fatal(err)
	}
	e.changed(t, listing)
	if v = e.get(t, buyer, v.ID); v.OpenPrice != nil || moveKinds(v)[len(v.Moves)-1] != domain.MoveListingClosed {
		t.Fatalf("%+v", v)
	}
	if _, err := e.catalog.Unhide(ctx, seller, listing); err != nil {
		t.Fatal(err)
	}
	e.changed(t, listing)
	if after := e.get(t, buyer, v.ID); len(after.Moves) != len(v.Moves) {
		t.Fatalf("unhide moved the deal: %+v", after)
	}
}

func TestListingChangedUnknownListing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.changed(t, uuid.NewV7())
}
