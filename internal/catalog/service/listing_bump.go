package service

import (
	"context"
	"fmt"
	"time"
	"uuid"
)

// Bump moves the seller's active listing to the top of the newest order. It
// writes no event: a bump is not a new listing, so no watch, follower or
// search engine hears of it.
func (c *Catalog) Bump(ctx context.Context, seller, id uuid.UUID, at time.Time) (bool, error) {
	ok, err := c.d.Listings.Bump(ctx, seller, id, at)
	if err != nil {
		return false, fmt.Errorf("bump: %w", err)
	}
	return ok, nil
}
