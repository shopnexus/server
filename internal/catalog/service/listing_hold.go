package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/events"
)

func (c *Catalog) ReportedListings(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]ReportedListing, error) {
	rows, err := c.listings.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get listings: %w", err)
	}
	out := make(map[uuid.UUID]ReportedListing, len(rows))
	for i := range rows {
		out[rows[i].ID()] = c.reported(&rows[i])
	}
	return out, nil
}

// Hold hides an active or hidden listing for a console member, with
// listing.hidden and the audit row in the listing's transaction. A deleted
// listing is domain.ErrNotFound, one of the ShopNexus account's
// accountdomain.ErrSystemAccount.
func (c *Catalog) Hold(ctx context.Context, id uuid.UUID, by audit.Actor) (ReportedListing, error) {
	l, err := c.listing(ctx, id)
	if err != nil {
		return ReportedListing{}, err
	}
	if l.Status() == domain.StatusDeleted {
		return ReportedListing{}, domain.ErrNotFound
	}
	if l.SellerID() == accountdomain.ShopNexusID {
		return ReportedListing{}, accountdomain.ErrSystemAccount
	}
	now := c.now()
	if err = l.Hold(now); err != nil {
		return ReportedListing{}, err
	}
	err = c.listings.Save(ctx, &l, uuid.NewV7(), &audit.Entry{
		Actor: by, Action: audit.ListingHide, Target: audit.Listing(id), Details: map[string]any{"account_id": l.SellerID()}, At: now,
	}, events.Event{ID: uuid.NewV7(), Kind: events.ListingHidden, SubjectID: id, OccurredAt: now})
	if err != nil {
		return ReportedListing{}, saveErr(err)
	}
	return c.reported(&l), nil
}

// Lift ends a member's hold, with listing.unhidden when the listing goes
// back on sale and the audit row in the listing's transaction. A deleted
// listing is domain.ErrNotFound.
func (c *Catalog) Lift(ctx context.Context, id uuid.UUID, by audit.Actor) (ReportedListing, error) {
	l, err := c.listing(ctx, id)
	if err != nil {
		return ReportedListing{}, err
	}
	if l.Status() == domain.StatusDeleted {
		return ReportedListing{}, domain.ErrNotFound
	}
	now := c.now()
	if err = l.Lift(now); err != nil {
		return ReportedListing{}, err
	}
	var evs []events.Event
	if l.Status() == domain.StatusActive {
		evs = append(evs, events.Event{ID: uuid.NewV7(), Kind: events.ListingUnhidden, SubjectID: id, OccurredAt: now})
	}
	err = c.listings.Save(ctx, &l, uuid.NewV7(), &audit.Entry{
		Actor: by, Action: audit.ListingUnhide, Target: audit.Listing(id), Details: map[string]any{"account_id": l.SellerID()}, At: now,
	}, evs...)
	if err != nil {
		return ReportedListing{}, saveErr(err)
	}
	return c.reported(&l), nil
}

// reported is l as console members see it.
func (c *Catalog) reported(l *domain.Listing) ReportedListing {
	r := ReportedListing{ID: l.ID(), Title: l.Title(), Price: l.Price(), Status: l.Status(), CreatedAt: l.CreatedAt()}
	if p := l.Photos(); len(p) > 0 && p[0].Keys.Thumb != "" {
		r.PhotoURL = c.media.URL(p[0].Keys.Thumb)
	}
	return r
}

// saveErr is a failed Save as a use case answers it: a listing written
// meanwhile is apperr.ErrTryAgain.
func saveErr(err error) error {
	if errors.Is(err, repository.ErrConflict) {
		return apperr.ErrTryAgain
	}
	return fmt.Errorf("save listing: %w", err)
}
