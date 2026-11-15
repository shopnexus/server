package service

import (
	"context"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// SellerListings lists what anyone sees of a seller's listings as search
// items, newest first: each measured from near when it names a position,
// and marked saved for a signed-in viewer.
func (d *Discovery) SellerListings(ctx context.Context, viewer *uuid.UUID, seller uuid.UUID, near catalogdomain.Near, p page.Request) (page.Page[SearchItem], error) {
	cards, err := d.catalog.SellerListings(ctx, seller, near, p)
	if err != nil {
		return page.Page[SearchItem]{}, fmt.Errorf("list the listings: %w", err)
	}
	items, err := d.withSaved(ctx, viewer, cards.Items)
	if err != nil {
		return page.Page[SearchItem]{}, err
	}
	return page.Page[SearchItem]{Items: items, NextCursor: cards.NextCursor}, nil
}
