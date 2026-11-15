package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// SellerCounts is how many listings a seller has sold, in a domain.Public
// status and in a domain.OnSale status, which domain.MaxOnSale caps.
type SellerCounts struct {
	Sold   int
	Public int
	OnSale int
}

// SellerListingPage is the SellerListingList schema in
// api/schemas/catalog.yaml: a page of a seller's public listings and, with a
// position, how far each is. The distances sit beside the items because
// they depend on the request, and a ListingView reads the same for everyone.
type SellerListingPage struct {
	page.Page[ListingView]

	Distances []ListingDistance `json:"distances,omitempty"`
}

// ListingDistance is how far one listing is from the reader's position, in
// whole kilometres.
type ListingDistance struct {
	ListingID  uuid.UUID `json:"listing_id"`
	DistanceKm int       `json:"distance_km"`
}

// SellerListings lists the seller's active and reserved listings, newest
// first, each with its distance from near when near names a position. An
// unknown seller is accountdomain.ErrAccountNotFound.
func (c *Catalog) SellerListings(ctx context.Context, seller uuid.UUID, near domain.Near, p page.Request) (SellerListingPage, error) {
	origin, err := c.Origin(ctx, near)
	if err != nil {
		return SellerListingPage{}, err
	}
	if _, err = c.accounts.Get(ctx, seller); err != nil {
		return SellerListingPage{}, fmt.Errorf("read the seller: %w", err)
	}
	rows, err := c.listings.ListBySeller(ctx, seller, domain.Public, p)
	if err != nil {
		return SellerListingPage{}, fmt.Errorf("list listings: %w", err)
	}
	items, err := c.views(ctx, rows)
	if err != nil {
		return SellerListingPage{}, err
	}
	out := SellerListingPage{Page: page.Of(items, p, func(v ListingView) uuid.UUID { return v.ID })}
	if origin != nil {
		out.Distances = make([]ListingDistance, len(out.Items))
		for i := range out.Items {
			out.Distances[i] = ListingDistance{ListingID: rows[i].ID(), DistanceKm: domain.DistanceKm(*origin, rows[i].Point())}
		}
	}
	return out, nil
}

// SellerCounts counts the seller's sold listings, whether through an order
// or marked by hand, the public ones and those under the cap.
func (c *Catalog) SellerCounts(ctx context.Context, seller uuid.UUID) (SellerCounts, error) {
	n, err := c.listings.CountByStatus(ctx, seller)
	if err != nil {
		return SellerCounts{}, fmt.Errorf("count listings: %w", err)
	}
	return SellerCounts{
		Sold:   n[domain.StatusSold],
		Public: domain.CountIn(n, domain.Public),
		OnSale: domain.CountIn(n, domain.OnSale),
	}, nil
}
