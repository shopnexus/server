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

// op is a seller's status change of their own listing.
type op int

const (
	opHide op = iota
	opUnhide
	opMarkSold
	opUnmarkSold
	opDelete
)

// Hide is UC-LST-02.
func (c *Catalog) Hide(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	return c.moveView(ctx, seller, id, opHide, t)
}

// Unhide is UC-LST-02; a listing a member holds is listing_held.
func (c *Catalog) Unhide(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	return c.moveView(ctx, seller, id, opUnhide, t)
}

// MarkSold is UC-LST-02.
func (c *Catalog) MarkSold(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	return c.moveView(ctx, seller, id, opMarkSold, t)
}

// UnmarkSold is UC-LST-02 A2, E6.
func (c *Catalog) UnmarkSold(ctx context.Context, seller, id uuid.UUID, t locale.Tag) (ListingView, error) {
	return c.moveView(ctx, seller, id, opUnmarkSold, t)
}

// Delete is UC-LST-02.
func (c *Catalog) Delete(ctx context.Context, seller, id uuid.UUID) error {
	_, err := c.move(ctx, seller, id, opDelete)
	return err
}

// moveView is move answered with the listing's view in t.
func (c *Catalog) moveView(ctx context.Context, seller, id uuid.UUID, o op, t locale.Tag) (ListingView, error) {
	l, err := c.move(ctx, seller, id, o)
	if err != nil {
		return ListingView{}, err
	}
	return c.view(ctx, l, t)
}

// move applies o to the seller's listing and saves it with o's event.
func (c *Catalog) move(ctx context.Context, seller, id uuid.UUID, o op) (domain.Listing, error) {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return domain.Listing{}, err
	}
	now := c.d.Now()
	var kind events.Kind
	switch o {
	case opHide:
		err, kind = l.Hide(now), events.ListingHidden
	case opUnhide:
		err, kind = l.Unhide(now), events.ListingUnhidden
	case opMarkSold:
		err, kind = l.MarkSold(now), events.ListingSold
	case opUnmarkSold:
		err, kind = l.UnmarkSold(now), events.ListingUnsold
	case opDelete:
		err, kind = l.Delete(now), events.ListingDeleted
	default:
	}
	if err != nil {
		return domain.Listing{}, err
	}
	return l, c.save(ctx, l, kind, now, uuid.NewV7())
}

// Reprice is UC-LST-04 A3: it sets the price of the seller's listing alone,
// as an edit of the price would, with listing.updated; the same price
// writes nothing.
func (c *Catalog) Reprice(ctx context.Context, seller, id uuid.UUID, price int64) error {
	l, err := c.own(ctx, seller, id)
	if err != nil {
		return err
	}
	now := c.d.Now()
	changed, err := l.Reprice(price, now)
	if err != nil || !changed {
		return err
	}
	return c.save(ctx, l, events.ListingUpdated, now, uuid.NewV7())
}

func (c *Catalog) Reserve(ctx context.Context, listingID, orderID uuid.UUID) error {
	l, err := c.listing(ctx, listingID)
	if err != nil {
		return err
	}
	now := c.d.Now()
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
	now := c.d.Now()
	if !l.Release(orderID, now) {
		return nil
	}
	return c.save(ctx, l, events.ListingReleased, now, uuid.NewV7())
}

func (c *Catalog) ReleaseHidden(ctx context.Context, listingID, orderID uuid.UUID) error {
	l, err := c.listing(ctx, listingID)
	if err != nil {
		return err
	}
	now := c.d.Now()
	if !l.ReleaseHidden(orderID, now) {
		return nil
	}
	return c.save(ctx, l, events.ListingHidden, now, uuid.NewV7())
}

func (c *Catalog) SellTo(ctx context.Context, listingID, orderID, writeID uuid.UUID) error {
	l, err := c.listing(ctx, listingID)
	if err != nil {
		return err
	}
	now := c.d.Now()
	if !l.SellTo(orderID, now) {
		return nil
	}
	return c.save(ctx, l, events.ListingSold, now, writeID)
}

// listing loads any listing by ID; an unknown one is domain.ErrNotFound.
func (c *Catalog) listing(ctx context.Context, id uuid.UUID) (domain.Listing, error) {
	l, err := c.d.Listings.Get(ctx, id)
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
	if err := c.d.Listings.Save(ctx, &l, writeID, nil, events.Event{ID: uuid.NewV7(), Kind: kind, SubjectID: l.ID(), OccurredAt: now}); err != nil {
		return saveErr(err)
	}
	return nil
}
