package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
)

func (c *Catalog) MarketPrice(ctx context.Context, listing uuid.UUID, textMin, photoMin float64) (MarketPrice, bool, error) {
	l, err := c.listings.Get(ctx, listing)
	if errors.Is(err, repository.ErrNotFound) {
		return MarketPrice{}, false, nil
	}
	if err != nil {
		return MarketPrice{}, false, fmt.Errorf("get listing: %w", err)
	}
	text, photo, err := c.vectors.Source(ctx, listing, c.textVersion, c.imageVersion)
	if err != nil {
		return MarketPrice{}, false, fmt.Errorf("read the listing's vectors: %w", err)
	}
	samples, err := c.PriceSample(ctx, PriceQuery{
		Exclude: l.SellerID(), CategoryCode: l.CategoryCode(), Condition: l.Condition(),
		Text: text, Photo: photo, TextMin: textMin, PhotoMin: photoMin, Now: c.now(),
	})
	if err != nil {
		return MarketPrice{}, false, err
	}
	return MarketPrice{SellerID: l.SellerID(), Price: l.Price(), Median: domain.MedianPrice(samples), SampleSize: len(samples)}, true, nil
}
