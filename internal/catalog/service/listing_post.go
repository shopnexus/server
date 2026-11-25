package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/locale"
)

// Post is UC-LST-01, step 3: a key the seller posted with already answers
// that listing as it is now (NFR-IDEM-01); otherwise it runs ListingPost,
// then reads the listing it posted.
func (c *Catalog) Post(ctx context.Context, seller, key uuid.UUID, d domain.ListingInput, t locale.Tag) (ListingView, error) {
	id, err := c.listings.ByKey(ctx, seller, key)
	if errors.Is(err, repository.ErrNotFound) {
		res, callErr := c.post.Call(ctx, PostInput{Seller: seller, Key: key, Listing: d})
		if callErr != nil {
			return ListingView{}, callErr
		}
		id, err = res.ListingID, nil
	}
	if err != nil {
		return ListingView{}, fmt.Errorf("find the key's listing: %w", err)
	}
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, fmt.Errorf("read the posted listing: %w", err)
	}
	return c.view(ctx, l, t)
}
