package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

// A seller's public list holds the active and reserved listings as cards,
// newest first, and the counts set them apart from the sold ones, a sold
// listing deleted since among them.
func TestSellerListingsAndCounts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	sold := e.active(t, seller).ID
	if _, err := e.catalog.MarkSold(ctx, seller, sold, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	soldDeleted := e.active(t, seller).ID
	if _, err := e.catalog.MarkSold(ctx, seller, soldDeleted, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{soldDeleted, e.active(t, seller).ID} {
		if err := e.catalog.Delete(ctx, seller, id); err != nil {
			t.Fatal(err)
		}
	}
	hidden := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, hidden, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	active := e.active(t, seller).ID
	reserved := e.active(t, seller).ID
	if err := e.catalog.Reserve(ctx, reserved, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	first, err := e.catalog.SellerListings(ctx, seller, domain.Near{}, nil, page.Request{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != reserved || first.NextCursor == nil || first.Items[0].DistanceKm != nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	if c := first.Items[0]; c.SellerID != seller || c.Commune == "" || c.CategoryCode == "" || c.CoverThumbURL == "" {
		t.Fatalf("a card misses its fields: %+v", c)
	}
	rest, err := e.catalog.SellerListings(ctx, seller, domain.Near{Commune: "00004"}, nil, page.Request{Cursor: *first.NextCursor, Limit: 1})
	if err != nil || len(rest.Items) != 1 || rest.Items[0].ID != active || rest.NextCursor != nil ||
		rest.Items[0].DistanceKm == nil || *rest.Items[0].DistanceKm != 0 {
		t.Fatalf("second page: %+v %v", rest, err)
	}
	left, err := e.catalog.SellerListings(ctx, seller, domain.Near{}, []uuid.UUID{reserved}, page.Request{})
	if err != nil || len(left.Items) != 1 || left.Items[0].ID != active {
		t.Fatalf("leaving the reserved one out: %+v %v", left, err)
	}
	if _, err = e.catalog.SellerListings(ctx, seller, domain.Near{Commune: "nope"}, nil, page.Request{}); !errors.Is(err, domain.ErrInvalidPosition) {
		t.Fatalf("an unknown commune: %v", err)
	}
	if got, err := e.catalog.SellerCounts(ctx, seller); err != nil || got != (service.SellerCounts{Sold: 2, Public: 2, OnSale: 3}) {
		t.Fatalf("counts: %+v %v", got, err)
	}
	for name, near := range map[string]domain.Near{"no position": {}, "an unknown commune": {Commune: "nope"}} {
		if _, err := e.catalog.SellerListings(ctx, uuid.NewV7(), near, nil, page.Request{}); !errors.Is(err, accountdomain.ErrAccountNotFound) {
			t.Errorf("unknown seller, %s: %v", name, err)
		}
	}
	empty, emptyErr := e.catalog.SellerListings(ctx, e.seller(t), domain.Near{Commune: "00004"}, nil, page.Request{})
	if emptyErr != nil || empty.Items == nil || len(empty.Items) != 0 {
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
		v, got, err := e.catalog.GetNear(ctx, nil, id, c.near, locale.Vietnamese)
		if err != nil || v.ID != id || (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
	for name, near := range map[string]domain.Near{
		"a latitude alone":   {Lat: 21.03},
		"out of range":       {Lat: 95, Lng: 105},
		"an unknown commune": {Commune: "nope"},
	} {
		if _, _, err := e.catalog.GetNear(ctx, nil, id, near, locale.Vietnamese); !errors.Is(err, domain.ErrInvalidPosition) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := e.catalog.GetNear(ctx, nil, uuid.NewV7(), domain.Near{Commune: "nope"}, locale.Vietnamese); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an unknown listing: %v", err)
	}
	hidden := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, hidden, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.catalog.GetNear(ctx, nil, hidden, domain.Near{Commune: "nope"}, locale.Vietnamese); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a hidden listing signed out: %v", err)
	}
}

// The sitemap names a public listing by its slug and its seller by ID, in
// one part each, the first without a cursor.
func TestSitemap(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := e.seller(t)
	l := e.active(t, seller)
	hidden := e.active(t, seller).ID
	if _, err := e.catalog.Hide(ctx, seller, hidden, locale.Vietnamese); err != nil {
		t.Fatal(err)
	}
	m, err := e.catalog.Sitemap(ctx)
	if err != nil || len(m.Listings) != 1 || len(m.Sellers) != 1 || m.Listings[0].Cursor != nil || m.Sellers[0].Cursor != nil {
		t.Fatalf("parts: %+v %v", m, err)
	}
	listings, err := e.catalog.SitemapListings(ctx, uuid.UUID{})
	if err != nil || len(listings) != 1 || listings[0].Slug != l.Slug || !listings[0].UpdatedAt.Equal(m.Listings[0].UpdatedAt) {
		t.Fatalf("listings: %+v %v, want %s", listings, err, l.Slug)
	}
	if len(l.Photos) != 1 || l.Photos[0].URL == "" || !slices.Equal(listings[0].Photos, []string{l.Photos[0].URL}) {
		t.Fatalf("photos: %v, want those of %+v", listings[0].Photos, l.Photos)
	}
	sellers, err := e.catalog.SitemapSellers(ctx, uuid.UUID{})
	if err != nil || len(sellers) != 1 || sellers[0].ID != seller {
		t.Fatalf("sellers: %+v %v", sellers, err)
	}
	if areas, areasErr := e.catalog.SitemapAreas(ctx); areasErr != nil || len(areas) != 0 {
		t.Fatalf("one listing makes no area: %+v %v", areas, areasErr)
	}
	if cats, catsErr := e.catalog.SitemapCategories(ctx); catsErr != nil || len(cats) != 0 {
		t.Fatalf("one listing makes no subcategory page: %+v %v", cats, catsErr)
	}
	for range 4 {
		catalogtest.Listing(t, e.db, seller, func(d *domain.ListingInput, _ *domain.Refs) { d.CategoryCode = "phones.android" })
	}
	areas, err := e.catalog.SitemapAreas(ctx)
	if err != nil || len(areas) != 1 || areas[0].CategoryCode != "phones" || areas[0].ProvinceCode != "01" || areas[0].Count != 5 {
		t.Fatalf("areas: %+v %v", areas, err)
	}
	// The root's five span two subcategories; a fifth Android makes a page.
	catalogtest.Listing(t, e.db, seller, func(d *domain.ListingInput, _ *domain.Refs) { d.CategoryCode = "phones.android" })
	cats, err := e.catalog.SitemapCategories(ctx)
	if err != nil || len(cats) != 1 || cats[0].Code != "phones.android" {
		t.Fatalf("subcategories: %+v %v", cats, err)
	}
}
