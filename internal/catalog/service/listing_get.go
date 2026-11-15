package service

import (
	"context"
	"fmt"
	"slices"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Get reads a listing. Anyone sees an active or reserved listing; its seller
// also sees it in any other state but deleted.
func (c *Catalog) Get(ctx context.Context, viewer *uuid.UUID, id uuid.UUID) (ListingView, error) {
	v, _, err := c.GetNear(ctx, viewer, id, domain.Near{})
	return v, err
}

// GetNear reads a listing as Get does, with its distance from near in whole
// kilometres, nil without a position. The distance is measured from the
// listing's own point, as a search measures it, which no view carries. An
// unknown or unseen listing is domain.ErrNotFound before near is checked.
func (c *Catalog) GetNear(ctx context.Context, viewer *uuid.UUID, id uuid.UUID, near domain.Near) (ListingView, *int, error) {
	l, err := c.listing(ctx, id)
	if err != nil {
		return ListingView{}, nil, err
	}
	owner := viewer != nil && *viewer == l.SellerID()
	public := slices.Contains(domain.Public, l.Status())
	if l.Status() == domain.StatusDeleted || (!public && !owner) {
		return ListingView{}, nil, domain.ErrNotFound
	}
	origin, err := c.Origin(ctx, near)
	if err != nil {
		return ListingView{}, nil, err
	}
	v, err := c.view(ctx, l)
	if err != nil || origin == nil {
		return v, nil, err
	}
	return v, new(domain.DistanceKm(*origin, l.Point())), nil
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
	items, err := c.views(ctx, rows)
	if err != nil {
		return page.Page[ListingView]{}, err
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
			Status: l.Status(), ReservedFor: l.ReservedFor(), CategoryCode: l.CategoryCode(), Condition: l.Condition(),
			CommuneCode: l.CommuneCode(),
		}
		if p := l.Photos(); len(p) > 0 && p[0].Keys.Thumb != "" {
			s.CoverThumbURL = c.media.URL(p[0].Keys.Thumb)
		}
		out[l.ID()] = s
	}
	return out, nil
}

func (c *Catalog) Summary(ctx context.Context, id uuid.UUID) (Summary, bool, error) {
	got, err := c.Summaries(ctx, []uuid.UUID{id})
	if err != nil {
		return Summary{}, false, err
	}
	s, ok := got[id]
	return s, ok, nil
}
