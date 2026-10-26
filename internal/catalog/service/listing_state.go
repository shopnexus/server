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
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.Hide(now); err != nil {
		return ListingView{}, err
	}
	return c.save(ctx, l, events.ListingHidden, now)
}

func (c *Catalog) Unhide(ctx context.Context, seller, id uuid.UUID) (ListingView, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.Unhide(now); err != nil {
		return ListingView{}, err
	}
	return c.save(ctx, l, events.ListingUnhidden, now)
}

func (c *Catalog) MarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.MarkSold(now); err != nil {
		return ListingView{}, err
	}
	return c.save(ctx, l, events.ListingSold, now)
}

func (c *Catalog) UnmarkSold(ctx context.Context, seller, id uuid.UUID) (ListingView, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.UnmarkSold(now); err != nil {
		return ListingView{}, err
	}
	return c.save(ctx, l, events.ListingUnsold, now)
}

func (c *Catalog) Delete(ctx context.Context, seller, id uuid.UUID) error {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return err
	}
	now := c.now()
	if err = l.Delete(now); err != nil {
		return err
	}
	_, err = c.save(ctx, l, events.ListingDeleted, now)
	return err
}

// save writes a status change of l with the event of that kind and returns
// the listing's view. A listing written meanwhile is apperr.ErrTryAgain.
func (c *Catalog) save(ctx context.Context, l domain.Listing, kind events.Kind, now time.Time) (ListingView, error) {
	err := c.listings.Save(ctx, &l, uuid.NewV7(), events.Event{ID: uuid.NewV7(), Kind: kind, SubjectID: l.ID(), OccurredAt: now})
	if errors.Is(err, repository.ErrConflict) {
		return ListingView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ListingView{}, fmt.Errorf("save listing: %w", err)
	}
	return c.view(ctx, l)
}
