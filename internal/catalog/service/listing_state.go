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

// Hide is UC-LST-02.
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

// Unhide is UC-LST-02.
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

// MarkSold is UC-LST-02.
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

// UnmarkSold is UC-LST-02 A2.
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

// Delete is UC-LST-02.
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

func (c *Catalog) Reserve(ctx context.Context, listingID, orderID uuid.UUID) error {
	l, err := c.listing(ctx, listingID)
	if err != nil {
		return err
	}
	now := c.now()
	changed, err := l.Reserve(orderID, now)
	if err != nil || !changed {
		return err
	}
	_, err = c.save(ctx, l, events.ListingReserved, now)
	return err
}

func (c *Catalog) Release(ctx context.Context, listingID, orderID uuid.UUID) error {
	l, err := c.listing(ctx, listingID)
	if err != nil {
		return err
	}
	now := c.now()
	if !l.Release(orderID, now) {
		return nil
	}
	_, err = c.save(ctx, l, events.ListingReleased, now)
	return err
}

func (c *Catalog) SellTo(ctx context.Context, listingID, orderID uuid.UUID) error {
	l, err := c.listing(ctx, listingID)
	if err != nil {
		return err
	}
	now := c.now()
	if !l.SellTo(orderID, now) {
		return nil
	}
	_, err = c.save(ctx, l, events.ListingSold, now)
	return err
}

// listing loads any listing by ID; an unknown one is domain.ErrNotFound.
func (c *Catalog) listing(ctx context.Context, id uuid.UUID) (domain.Listing, error) {
	l, err := c.listings.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Listing{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Listing{}, fmt.Errorf("get listing: %w", err)
	}
	return l, nil
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
