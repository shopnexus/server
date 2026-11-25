package service

import (
	"context"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// SellerListings lists what anyone sees of a seller's listings as search
// items, newest first: each measured from near when it names a position.
// A signed-in viewer's items leave out the listings they hid, as their
// searches do, and are marked saved.
func (d *Discovery) SellerListings(ctx context.Context, viewer *uuid.UUID, seller uuid.UUID, near catalogdomain.Near, p page.Request) (page.Page[SearchItem], error) {
	var hidden []uuid.UUID
	if viewer != nil {
		var err error
		if hidden, err = d.d.Hidden.List(ctx, *viewer); err != nil {
			return page.Page[SearchItem]{}, fmt.Errorf("read hides: %w", err)
		}
	}
	cards, err := d.d.Catalog.SellerListings(ctx, seller, near, hidden, p)
	if err != nil {
		return page.Page[SearchItem]{}, err //nolint:wrapcheck // catalog names its failed step and the caller names this one.
	}
	items, err := d.withSaved(ctx, viewer, cards.Items)
	if err != nil {
		return page.Page[SearchItem]{}, err
	}
	return page.Page[SearchItem]{Items: items, NextCursor: cards.NextCursor}, nil
}
