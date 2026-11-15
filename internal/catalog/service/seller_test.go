package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

// A seller's public list holds the active and reserved listings, newest
// first, and the counts set them apart from the sold ones.
func TestSellerListingsAndCounts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	sold := e.active(t, seller).ID
	if _, err := e.catalog.MarkSold(ctx, seller, sold); err != nil {
		t.Fatal(err)
	}
	hidden := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	active := e.active(t, seller).ID
	reserved := e.active(t, seller).ID
	if err := e.catalog.Reserve(ctx, reserved, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	first, err := e.catalog.SellerListings(ctx, seller, domain.Near{}, page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != reserved || first.NextCursor == nil || first.Distances != nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	rest, err := e.catalog.SellerListings(ctx, seller, domain.Near{Commune: "00004"}, page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(rest.Items) != 1 || rest.Items[0].ID != active || rest.NextCursor != nil ||
		len(rest.Distances) != 1 || rest.Distances[0] != (service.ListingDistance{ListingID: active, DistanceKm: 0}) {
		t.Fatalf("second page: %+v %v", rest, err)
	}
	if _, err = e.catalog.SellerListings(ctx, seller, domain.Near{Commune: "nope"}, page.Request{}); !errors.Is(err, domain.ErrInvalidPosition) {
		t.Fatalf("an unknown commune: %v", err)
	}
	if got, err := e.catalog.SellerCounts(ctx, seller); err != nil || got != (service.SellerCounts{Sold: 1, Public: 2, OnSale: 3}) {
		t.Fatalf("counts: %+v %v", got, err)
	}
	for name, near := range map[string]domain.Near{"no position": {}, "an unknown commune": {Commune: "nope"}} {
		if _, err := e.catalog.SellerListings(ctx, uuid.NewV7(), near, page.Request{}); !errors.Is(err, accountdomain.ErrAccountNotFound) {
			t.Errorf("unknown seller, %s: %v", name, err)
		}
	}
	// With a position an empty page has an empty list of distances.
	empty, emptyErr := e.catalog.SellerListings(ctx, e.seller(t), domain.Near{Commune: "00004"}, page.Request{})
	if emptyErr != nil || len(empty.Items) != 0 || empty.Distances == nil || len(empty.Distances) != 0 {
		t.Fatalf("an empty page with a position: %+v %v", empty, emptyErr)
	}
}

// GetNear measures from the listing's point and truncates to whole
// kilometres; no position gives none, and a bad one is refused once the
// listing is known to be seen.
func TestGetNear(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	id := e.active(t, seller).ID
	places, err := e.catalog.Places(ctx, []string{"00004"})
	if err != nil {
		t.Fatal(err)
	}
	home := places["00004"]
	for name, c := range map[string]struct {
		near domain.Near
		want *int
	}{
		"no position":         {domain.Near{}, nil},
		"its commune":         {domain.Near{Commune: "00004"}, new(0)},
		"0.05° north, 5.5 km": {domain.Near{Lat: home.Lat + 0.05, Lng: home.Lng}, new(5)},
	} {
		v, got, err := e.catalog.GetNear(ctx, nil, id, c.near)
		if err != nil || v.ID != id || (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
	for name, near := range map[string]domain.Near{
		"a latitude alone":   {Lat: 21.03},
		"out of range":       {Lat: 95, Lng: 105},
		"an unknown commune": {Commune: "nope"},
	} {
		if _, _, err := e.catalog.GetNear(ctx, nil, id, near); !errors.Is(err, domain.ErrInvalidPosition) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := e.catalog.GetNear(ctx, nil, uuid.NewV7(), domain.Near{Commune: "nope"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown listing: %v", err)
	}
	hidden := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, hidden); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.catalog.GetNear(ctx, nil, hidden, domain.Near{Commune: "nope"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a hidden listing signed out: %v", err)
	}
}
