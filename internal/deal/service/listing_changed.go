package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
)

// ListingChanged brings every deal on the listing in line with the
// listing's current state. It reads current state, so a repeated or late
// call changes nothing more. A deal a user moved meanwhile fails the call,
// and the worker's retry reads it again.
func (s *Deals) ListingChanged(ctx context.Context, listingID uuid.UUID) error {
	l, err := s.listing(ctx, listingID)
	if errors.Is(err, catalogdomain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	deals, err := s.deals.OfListing(ctx, listingID)
	if err != nil {
		return fmt.Errorf("list deals: %w", err)
	}
	now := s.now()
	for i := range deals {
		if !deals[i].ListingChanged(facts(l), now) {
			continue
		}
		if err = s.deals.Save(ctx, &deals[i], uuid.NewV7(), moved(deals[i].ID(), now)); err != nil {
			return fmt.Errorf("save deal: %w", err)
		}
	}
	return nil
}
