package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/provider/chotot"
)

// ListingSold announces the sale of an import's copy, unless the seller
// turned remind_hide off, no longer links the profile, or the listing left
// Chợ Tốt already. Chợ Tốt not answering still reminds.
func (c *Chotot) ListingSold(ctx context.Context, listing, event uuid.UUID, at time.Time) error {
	imp, found, err := c.d.Imports.ByListing(ctx, listing)
	if err != nil || !found {
		return err //nolint:wrapcheck // the store names it
	}
	l, linked, err := c.d.Links.Get(ctx, imp.AccountID)
	if err != nil || !linked || !l.RemindHide || l.OID != imp.OID {
		return err //nolint:wrapcheck // the store names it
	}
	adCtx, cancel := context.WithTimeout(ctx, c.d.CallTimeout)
	_, err = c.d.Source.Ad(adCtx, imp.ListID)
	cancel()
	if errors.Is(err, chotot.ErrNotFound) {
		return nil
	}
	return c.d.Imports.Sold(ctx, imp.ID, event, at) //nolint:wrapcheck // the store names it
}

// ListingEdited clears the import's review.
func (c *Chotot) ListingEdited(ctx context.Context, listing uuid.UUID) error {
	return c.d.Imports.ClearReview(ctx, listing) //nolint:wrapcheck // the store names it
}

// ListingDeleted forgets the import.
func (c *Chotot) ListingDeleted(ctx context.Context, listing uuid.UUID) error {
	return c.d.Imports.DeleteListing(ctx, listing) //nolint:wrapcheck // the store names it
}

// DeleteAccount removes the account's code, link and imports.
func (c *Chotot) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	return c.d.Links.DeleteAccount(ctx, account) //nolint:wrapcheck // the store names it
}

// LinkFacts reads a link for notification.
func (c *Chotot) LinkFacts(ctx context.Context, link uuid.UUID) (LinkFacts, bool, error) {
	l, found, err := c.d.Links.ByID(ctx, link)
	if err != nil || !found {
		return LinkFacts{}, false, err //nolint:wrapcheck // the store names it
	}
	return LinkFacts{ID: l.ID, Account: l.AccountID}, true, nil
}

// SaleFacts reads a sold copy for notification.
func (c *Chotot) SaleFacts(ctx context.Context, id uuid.UUID) (SaleFacts, bool, error) {
	imp, found, err := c.d.Imports.ByID(ctx, id)
	if err != nil || !found || imp.ListingID == nil {
		return SaleFacts{}, false, err //nolint:wrapcheck // the store names it
	}
	s, ok, err := c.d.Catalog.Summary(ctx, *imp.ListingID)
	if err != nil {
		return SaleFacts{}, false, fmt.Errorf("read the listing: %w", err)
	}
	if !ok {
		return SaleFacts{}, false, nil
	}
	return SaleFacts{ID: imp.ID, Account: imp.AccountID, ListingID: s.ID, Title: s.Title, ListID: imp.ListID}, true, nil
}
