package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/deal/domain"
)

// ListingChanged brings every deal on the listing in line with the
// listing's current state. It reads current state, so a repeated or late
// call changes nothing more. A deal a user moved meanwhile fails the call,
// and the worker's retry reads it again. A listing reserved for an order
// that is not shipped yet, while DealShip runs, counts as paused; DealShip
// calls this again once the order is shipped.
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
	rules := facts(l)
	if rules.Status == domain.ListingReserved && !slices.ContainsFunc(deals, func(d domain.Deal) bool {
		o := d.Order()
		return o != nil && o.ID == l.ReservedFor && o.Status == domain.OrderShipped
	}) {
		rules.Status = domain.ListingPaused
	}
	now := s.now()
	for i := range deals {
		if !deals[i].ListingChanged(rules, now) {
			continue
		}
		if err = s.deals.Save(ctx, &deals[i], uuid.NewV7(), moved(deals[i].ID(), now)); err != nil {
			return fmt.Errorf("save deal: %w", err)
		}
	}
	return nil
}
