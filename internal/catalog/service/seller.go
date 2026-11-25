package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// SellerCounts is how many listings a seller has sold, in a domain.Public
// status and in a domain.OnSale status, which domain.MaxOnSale caps.
type SellerCounts struct {
	Sold   int
	Public int
	OnSale int
}

// SellerListings lists the seller's active and reserved listings that are
// not in except as cards, newest first, each measured from near when near
// names a position. An unknown seller is
// accountdomain.ErrAccountNotFound, before near is checked. The seller's
// account is read only for an empty page, since a listing shows the seller
// exists.
func (c *Catalog) SellerListings(ctx context.Context, seller uuid.UUID, near domain.Near, except []uuid.UUID, p page.Request) (page.Page[SearchCard], error) {
	rows, err := c.d.Listings.ListBySeller(ctx, seller, domain.Public, except, p)
	if err != nil {
		return page.Page[SearchCard]{}, fmt.Errorf("list listings: %w", err)
	}
	if len(rows) == 0 {
		if _, err = c.d.Accounts.Get(ctx, seller); err != nil {
			return page.Page[SearchCard]{}, fmt.Errorf("read the seller: %w", err)
		}
	}
	origin, err := c.Origin(ctx, near)
	if err != nil {
		return page.Page[SearchCard]{}, err
	}
	kept := page.Of(rows, p, func(l domain.Listing) uuid.UUID { return l.ID() })
	cards, err := c.cards(ctx, kept.Items, origin)
	if err != nil {
		return page.Page[SearchCard]{}, err
	}
	return page.Page[SearchCard]{Items: cards, NextCursor: kept.NextCursor}, nil
}

// SellerCounts counts the seller's sold listings, whether through an order
// or marked by hand and deleted since or not, the public ones and those
// under the cap.
func (c *Catalog) SellerCounts(ctx context.Context, seller uuid.UUID) (SellerCounts, error) {
	n, err := c.d.Listings.CountByStatus(ctx, seller)
	if err != nil {
		return SellerCounts{}, fmt.Errorf("count listings: %w", err)
	}
	sold, err := c.d.Listings.CountSold(ctx, seller)
	if err != nil {
		return SellerCounts{}, fmt.Errorf("count sold listings: %w", err)
	}
	return SellerCounts{
		Sold:   sold,
		Public: domain.CountIn(n, domain.Public),
		OnSale: domain.CountIn(n, domain.OnSale),
	}, nil
}

func (c *Catalog) CountByStatus(ctx context.Context, since time.Time) (map[domain.Status]int, error) {
	n, err := c.d.Listings.CountCreatedSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("count listings: %w", err)
	}
	return n, nil
}
