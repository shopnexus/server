package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/locale"
)

// Hide is UC-LST-02.
func (c *Catalog) Hide(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.Hide(now); err != nil {
		return ListingView{}, err
	}
	if err = c.save(ctx, l, events.ListingHidden, now, uuid.NewV7()); err != nil {
		return ListingView{}, err
	}
	return c.view(ctx, l, t)
}

// Unhide is UC-LST-02.
func (c *Catalog) Unhide(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.Unhide(now); err != nil {
		return ListingView{}, err
	}
	if err = c.save(ctx, l, events.ListingUnhidden, now, uuid.NewV7()); err != nil {
		return ListingView{}, err
	}
	return c.view(ctx, l, t)
}

// MarkSold is UC-LST-02.
func (c *Catalog) MarkSold(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.MarkSold(now); err != nil {
		return ListingView{}, err
	}
	if err = c.save(ctx, l, events.ListingSold, now, uuid.NewV7()); err != nil {
		return ListingView{}, err
	}
	return c.view(ctx, l, t)
}

// UnmarkSold is UC-LST-02 A2.
func (c *Catalog) UnmarkSold(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return ListingView{}, err
	}
	now := c.now()
	if err = l.UnmarkSold(now); err != nil {
		return ListingView{}, err
	}
	if err = c.save(ctx, l, events.ListingUnsold, now, uuid.NewV7()); err != nil {
		return ListingView{}, err
	}
	return c.view(ctx, l, t)
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
	return c.save(ctx, l, events.ListingDeleted, now, uuid.NewV7())
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
	return c.save(ctx, l, events.ListingReserved, now, uuid.NewV7())
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
	return c.save(ctx, l, events.ListingReleased, now, uuid.NewV7())
}

func (c *Catalog) SellTo(ctx context.Context, listingID, orderID, writeID uuid.UUID) error {
	l, err := c.listing(ctx, listingID)
	if err != nil {
		return err
	}
	now := c.now()
	if !l.SellTo(orderID, now) {
		return nil
	}
	return c.save(ctx, l, events.ListingSold, now, writeID)
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

// save writes a status change of l under writeID with the event of that
// kind. A listing written meanwhile is apperr.ErrTryAgain.
func (c *Catalog) save(ctx context.Context, l domain.Listing, kind events.Kind, now time.Time, writeID uuid.UUID) error {
	if err := c.listings.Save(ctx, &l, writeID, nil, events.Event{ID: uuid.NewV7(), Kind: kind, SubjectID: l.ID(), OccurredAt: now}); err != nil {
		return saveErr(err)
	}
	return nil
}
