package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// Edit is UC-LST-02: it runs ListingEdit once per seller and
// idempotencyKey, then reads the listing.
func (c *Catalog) Edit(ctx context.Context, seller, id uuid.UUID, idempotencyKey string, d domain.ListingInput) (ListingView, error) {
	if _, err := c.edit.Call(ctx, seller.String()+"/"+idempotencyKey, EditInput{Seller: seller, ID: id, Listing: d}); err != nil {
		return ListingView{}, err
	}
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, fmt.Errorf("read the edited listing: %w", err)
	}
	return c.view(ctx, l)
}
