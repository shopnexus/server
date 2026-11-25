package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
)

// DeletedUploads returns the uploads listing id gave up when it was
// deleted; none for an unknown listing or one not deleted.
func (c *Catalog) DeletedUploads(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	l, err := c.listings.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get listing: %w", err)
	}
	return l.DeletedUploads(), nil
}

// leftBehind are the statuses a deleted seller's listings leave: sold ones
// stay for the orders that name them, and none is reserved, since erasure
// refuses a seller with a shipped order. A processing listing goes too;
// its publish flow finds it deleted and stops.
var leftBehind = []domain.Status{domain.StatusProcessing, domain.StatusActive, domain.StatusHidden, domain.StatusRejected}

// SellerLeft deletes the seller's drafts, then their listings a page at a
// time; a deleted listing leaves the filter, so each round reads the first
// page again. The drafts' photos expire on their own; the listings' go on
// listing.deleted (worker.ListingWatch).
func (c *Catalog) SellerLeft(ctx context.Context, seller uuid.UUID) error {
	if err := c.drafts.DeleteAll(ctx, seller); err != nil {
		return fmt.Errorf("delete drafts: %w", err)
	}
	for {
		ls, err := c.listings.ListBySeller(ctx, seller, leftBehind, nil, page.Request{Limit: 100})
		if err != nil {
			return fmt.Errorf("list listings: %w", err)
		}
		if len(ls) == 0 {
			return nil
		}
		now := c.now()
		for i := range ls {
			if err = ls[i].Delete(now); err != nil {
				return fmt.Errorf("delete listing %s: %w", ls[i].ID(), err)
			}
			err = c.listings.Save(ctx, &ls[i], uuid.NewV7(), nil,
				events.Event{ID: uuid.NewV7(), Kind: events.ListingDeleted, SubjectID: ls[i].ID(), OccurredAt: now})
			if errors.Is(err, repository.ErrConflict) {
				return apperr.ErrTryAgain
			}
			if err != nil {
				return fmt.Errorf("save listing: %w", err)
			}
		}
	}
}
