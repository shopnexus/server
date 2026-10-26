package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Get reads a listing. Anyone sees an active listing; its seller also sees
// it in any other state but deleted.
func (c *Catalog) Get(ctx context.Context, viewer *uuid.UUID, id uuid.UUID) (ListingView, error) {
	l, err := c.listings.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ListingView{}, domain.ErrNotFound
	}
	if err != nil {
		return ListingView{}, fmt.Errorf("get listing: %w", err)
	}
	owner := viewer != nil && *viewer == l.SellerID()
	if l.Status() == domain.StatusDeleted || (l.Status() != domain.StatusActive && !owner) {
		return ListingView{}, domain.ErrNotFound
	}
	return c.view(ctx, l)
}

// ListMine lists the listings of one of the seller's panes, newest first.
func (c *Catalog) ListMine(ctx context.Context, seller uuid.UUID, pane Pane, p page.Request) (page.Page[ListingView], error) {
	statuses := domain.OnSale
	if pane == PaneSold {
		statuses = []domain.Status{domain.StatusSold}
	}
	rows, err := c.listings.ListBySeller(ctx, seller, statuses, p)
	if err != nil {
		return page.Page[ListingView]{}, fmt.Errorf("list listings: %w", err)
	}
	items := make([]ListingView, 0, len(rows))
	for i := range rows {
		v, viewErr := c.view(ctx, rows[i])
		if viewErr != nil {
			return page.Page[ListingView]{}, viewErr
		}
		items = append(items, v)
	}
	return page.Of(items, p, func(v ListingView) uuid.UUID { return v.ID }), nil
}

// Summaries reads listings in any status, deleted included, for other
// modules.
func (c *Catalog) Summaries(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Summary, error) {
	rows, err := c.listings.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get listings: %w", err)
	}
	out := make(map[uuid.UUID]Summary, len(rows))
	for i := range rows {
		l := &rows[i]
		s := Summary{
			ID: l.ID(), SellerID: l.SellerID(), Title: l.Title(), Price: l.Price(), AllowOffers: l.AllowOffers(),
			Status: l.Status(),
		}
		if p := l.Photos(); len(p) > 0 && p[0].Keys.Thumb != "" {
			s.CoverThumbURL = c.media.URL(p[0].Keys.Thumb)
		}
		out[l.ID()] = s
	}
	return out, nil
}
