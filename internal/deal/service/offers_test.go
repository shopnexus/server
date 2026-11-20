package service_test

import (
	"context"
	"testing"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/page"
)

// An offer its listing voided drops out of the page before the worker
// records the change, and the cursor still follows the stored rows.
func TestOpenOffersLeaveOutVoided(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.person(t)
	deals := make([]service.DealView, 0, 3)
	for range 3 {
		buyer, listing := e.person(t), catalogtest.Listing(t, e.db, seller)
		d := e.open(t, buyer, listing)
		must(t)(e.deals.Offer(ctx, buyer, d.ID, 1_000_000))
		deals = append(deals, d)
	}
	older, middle, newest := deals[0], deals[1], deals[2]
	e.edit(t, newest.Listing.ID, func(d *catalogdomain.ListingInput) { d.Price = 1_400_000 })

	first, err := e.deals.OpenOffers(ctx, seller, page.Request{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != middle.ID || first.NextCursor == nil || *first.NextCursor != middle.ID {
		t.Fatalf("first page: %+v", first)
	}
	if r := first.Items[0].OfferRange; r == nil || r.Min != 1_001_000 {
		t.Fatalf("the seller's counter range: %+v", r)
	}
	next, err := e.deals.OpenOffers(ctx, seller, page.Request{Cursor: *first.NextCursor, Limit: 2})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != older.ID || next.NextCursor != nil {
		t.Fatalf("next page: %+v %v", next, err)
	}
	if n, err := e.deals.OpenOfferCount(ctx, seller); err != nil || n != 3 {
		t.Fatalf("count: %d %v", n, err)
	}
}
