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

// SellerListings lists the seller's active and reserved listings as cards,
// newest first, each measured from near when near names a position. An
// unknown seller is accountdomain.ErrAccountNotFound, before near is
// checked.
func (c *Catalog) SellerListings(ctx context.Context, seller uuid.UUID, near domain.Near, p page.Request) (page.Page[SearchCard], error) {
	if _, err := c.accounts.Get(ctx, seller); err != nil {
		return page.Page[SearchCard]{}, fmt.Errorf("read the seller: %w", err)
	}
	origin, err := c.Origin(ctx, near)
	if err != nil {
		return page.Page[SearchCard]{}, err
	}
	rows, err := c.listings.ListBySeller(ctx, seller, domain.Public, p)
	if err != nil {
		return page.Page[SearchCard]{}, fmt.Errorf("list listings: %w", err)
	}
	vs, err := c.views(ctx, rows)
	if err != nil {
		return page.Page[SearchCard]{}, err
	}
	cards := make([]SearchCard, len(vs))
	for i := range vs {
		cards[i] = cardOf(&vs[i], rows[i].Point(), origin)
	}
	return page.Of(cards, p, func(c SearchCard) uuid.UUID { return c.ID }), nil
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
