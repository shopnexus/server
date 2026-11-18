package service

import (
	"context"
	"fmt"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// PriceDropFacts is a save whose listing fell to a new low.
type PriceDropFacts struct {
	ID         uuid.UUID `json:"id"`
	Account    uuid.UUID `json:"account"`
	ListingID  uuid.UUID `json:"listing_id"`
	Title      string    `json:"title"`
	SavedPrice int64     `json:"saved_price"`
}

// SetAlert saves the listing, a second time changing nothing, then sets
// the level. An unsave between the two is try_again.
func (d *Discovery) SetAlert(ctx context.Context, account, listing uuid.UUID, below int64) error {
	if err := d.Save(ctx, account, listing); err != nil {
		return err
	}
	found, err := d.saves.SetAlert(ctx, account, listing, &below)
	if err != nil {
		return fmt.Errorf("set alert: %w", err)
	}
	if !found {
		return apperr.ErrTryAgain
	}
	return nil
}

func (d *Discovery) ClearAlert(ctx context.Context, account, listing uuid.UUID) error {
	if _, err := d.saves.SetAlert(ctx, account, listing, nil); err != nil {
		return fmt.Errorf("clear alert: %w", err)
	}
	return nil
}

// PriceMoved reads the listing as it is now, so events out of order or late
// tell at the current price; a listing off sale tells no one.
func (d *Discovery) PriceMoved(ctx context.Context, listing uuid.UUID) error {
	l, ok, err := d.catalog.Summary(ctx, listing)
	if err != nil {
		return fmt.Errorf("read listing: %w", err)
	}
	if !ok || l.Status != catalogdomain.StatusActive {
		return nil
	}
	if _, err = d.saves.PriceMoved(ctx, listing, l.Price, d.now()); err != nil {
		return fmt.Errorf("weigh the saves: %w", err)
	}
	return nil
}

func (d *Discovery) PriceDropFacts(ctx context.Context, id uuid.UUID) (PriceDropFacts, bool, error) {
	s, found, err := d.saves.Get(ctx, id)
	if err != nil {
		return PriceDropFacts{}, false, fmt.Errorf("read save: %w", err)
	}
	if !found {
		return PriceDropFacts{}, false, nil
	}
	l, ok, err := d.catalog.Summary(ctx, s.ListingID)
	if err != nil {
		return PriceDropFacts{}, false, fmt.Errorf("read listing: %w", err)
	}
	if !ok {
		return PriceDropFacts{}, false, nil
	}
	return PriceDropFacts{ID: id, Account: s.Account, ListingID: s.ListingID, Title: l.Title, SavedPrice: s.Price}, true, nil
}
