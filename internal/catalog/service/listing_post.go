package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// Post runs ListingPost once per seller and idempotencyKey, then reads the
// listing it posted. A repeated key gets the first post's listing as it is
// now.
func (c *Catalog) Post(ctx context.Context, seller uuid.UUID, idempotencyKey string, d domain.Draft) (ListingView, error) {
	res, err := c.post.Call(ctx, seller.String()+"/"+idempotencyKey, PostInput{Seller: seller, Draft: d})
	if err != nil {
		return ListingView{}, err
	}
	l, err := c.own(ctx, seller, res.ListingID)
	if err != nil {
		return ListingView{}, fmt.Errorf("read the posted listing: %w", err)
	}
	return c.view(ctx, l)
}
