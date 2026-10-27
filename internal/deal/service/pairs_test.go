package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
)

func TestPairSummaries(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	viewer, a, b, c, d := e.person(t), e.person(t), e.person(t), e.person(t), e.person(t)

	// With a: the viewer's offer waits for a; a's offer waits for the
	// viewer; a newer deal has nothing open.
	waiting := e.open(t, viewer, e.listing(t, a))
	must(t)(e.deals.Offer(ctx, viewer, waiting.ID, 1_000_000))
	facing := e.open(t, a, e.listing(t, viewer))
	must(t)(e.deals.Offer(ctx, a, facing.ID, 1_100_000))
	e.open(t, viewer, e.listing(t, a))
	// With c: an idle deal, then a requested order the seller has to ship.
	e.open(t, viewer, e.listing(t, c))
	requested := e.requested(t, viewer, e.listing(t, c))
	// With d: an agreement waiting for the viewer's COD request.
	agreed := e.agreed(t, viewer, e.listing(t, d))

	got, err := e.deals.PairSummaries(ctx, viewer, []uuid.UUID{a, b, c, d})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d pairs: %+v", len(got), got)
	}
	if _, ok := got[b]; ok {
		t.Fatal("a pair without a deal has a summary")
	}
	ga := got[a]
	if ga.Deal.ID != facing.ID || !ga.Deal.YourTurn || ga.Deal.Role != domain.Seller || ga.OtherActive != 2 ||
		ga.Deal.OpenPrice == nil || ga.Deal.OpenPrice.Amount != 1_100_000 || ga.Deal.Listing.Title != "Xe đạp Asama" {
		t.Fatalf("with a: %+v", ga)
	}
	gc := got[c]
	if gc.Deal.ID != requested.ID || gc.Deal.YourTurn || gc.Deal.Status != domain.StatusAgreed ||
		gc.Deal.OrderStatus != domain.OrderRequested || gc.Deal.AgreedPrice == nil || *gc.Deal.AgreedPrice != 1_500_000 ||
		gc.OtherActive != 1 {
		t.Fatalf("with c: %+v", gc)
	}
	gd := got[d]
	if gd.Deal.ID != agreed.ID || !gd.Deal.YourTurn || gd.Deal.Role != domain.Buyer || gd.Deal.OrderStatus != "" ||
		gd.OtherActive != 0 {
		t.Fatalf("with d: %+v", gd)
	}
	// The seller of the requested order has to ship it.
	if s, err := e.deals.PairSummaries(ctx, c, []uuid.UUID{viewer}); err != nil || !s[viewer].Deal.YourTurn ||
		s[viewer].Deal.ID != requested.ID {
		t.Fatalf("as the seller: %+v %v", s, err)
	}
}
