package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/events"
)

func (c *Catalog) Hide(ctx context.Context, seller, id uuid.UUID) (ListingView, error) {
	return c.move(ctx, seller, id, (*domain.Listing).Hide, events.ListingHidden)
}

func (c *Catalog) Unhide(ctx context.Context, seller, id uuid.UUID) (ListingView, error) {
	return c.move(ctx, seller, id, (*domain.Listing).Unhide, events.ListingUnhidden)
}

func (c *Catalog) MarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error) {
	return c.move(ctx, seller, id, (*domain.Listing).MarkSold, events.ListingSold)
}

func (c *Catalog) UnmarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error) {
	return c.move(ctx, seller, id, (*domain.Listing).UnmarkSold, events.ListingUnsold)
}

func (c *Catalog) Delete(ctx context.Context, seller, id uuid.UUID) error {
	_, err := c.move(ctx, seller, id, (*domain.Listing).Delete, events.ListingDeleted)
	return err
}

// move applies a status change to the seller's listing and saves it with
// the event of that kind.
func (c *Catalog) move(ctx context.Context, seller, id uuid.UUID, change func(*domain.Listing, time.Time) error,
	kind events.Kind,
) (ListingView, error) {
	now := c.now()
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	if err = change(&l, now); err != nil {
		return ListingView{}, err
	}
	err = c.listings.Save(ctx, &l, uuid.NewV7(), events.Event{ID: uuid.NewV7(), Kind: kind, SubjectID: id, OccurredAt: now})
	if errors.Is(err, repository.ErrConflict) {
		return ListingView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ListingView{}, fmt.Errorf("save listing: %w", err)
	}
	return c.view(ctx, l)
}
