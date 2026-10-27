package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// SellerCounts is how many listings a seller has sold and has on sale.
type SellerCounts struct {
	Sold   int
	OnSale int
}

// SellerListings lists the seller's active and reserved listings, newest
// first. An unknown seller is accountdomain.ErrAccountNotFound.
func (c *Catalog) SellerListings(ctx context.Context, seller uuid.UUID, p page.Request) (page.Page[ListingView], error) {
	if _, err := c.accounts.Get(ctx, seller); err != nil {
		return page.Page[ListingView]{}, fmt.Errorf("read the seller: %w", err)
	}
	rows, err := c.listings.ListBySeller(ctx, seller, domain.Public, p)
	if err != nil {
		return page.Page[ListingView]{}, fmt.Errorf("list listings: %w", err)
	}
	items, err := c.views(ctx, rows)
	if err != nil {
		return page.Page[ListingView]{}, err
	}
	return page.Of(items, p, func(v ListingView) uuid.UUID { return v.ID }), nil
}

// SellerCounts counts the seller's sold listings, whether through an order
// or marked by hand, and the public ones.
func (c *Catalog) SellerCounts(ctx context.Context, seller uuid.UUID) (SellerCounts, error) {
	n, err := c.listings.CountByStatus(ctx, seller)
	if err != nil {
		return SellerCounts{}, fmt.Errorf("count listings: %w", err)
	}
	out := SellerCounts{Sold: n[domain.StatusSold]}
	for _, st := range domain.Public {
		out.OnSale += n[st]
	}
	return out, nil
}
