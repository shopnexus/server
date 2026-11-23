package service

import (
	"context"
	"fmt"
	"slices"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

// Get reads a listing, its specs named in t. Anyone sees an active or
// reserved listing; its seller also sees it in any other state but deleted.
func (c *Catalog) Get(ctx context.Context, viewer *uuid.UUID, id uuid.UUID, t locale.Tag) (ListingView, error) {
	v, _, err := c.GetNear(ctx, viewer, id, domain.Near{}, t)
	return v, err
}

// GetNear reads a listing as Get does, with its distance from near in whole
// kilometres, nil without a position. The distance is measured from the
// listing's own point, as a search measures it, which no view carries. An
// unknown or unseen listing is domain.ErrNotFound before near is checked.
func (c *Catalog) GetNear(ctx context.Context, viewer *uuid.UUID, id uuid.UUID, near domain.Near, t locale.Tag) (ListingView, *int, error) {
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
	v, err := c.view(ctx, l, t)
	if err != nil || origin == nil {
		return v, nil, err
	}
	return v, new(domain.DistanceKm(*origin, l.Point())), nil
}

// GetSold reads a listing that sold, through an order or marked by hand.
// One hidden by ShopNexus after the sale is hidden, so it is
// domain.ErrNotFound like every other status.
func (c *Catalog) GetSold(ctx context.Context, id uuid.UUID, t locale.Tag) (ListingView, error) {
	l, err := c.listing(ctx, id)
	if err != nil {
		return ListingView{}, err
	}
	if l.Status() != domain.StatusSold {
		return ListingView{}, domain.ErrNotFound
	}
	return c.view(ctx, l, t)
}

// ListMine lists the listings of one of the seller's panes, newest first.
func (c *Catalog) ListMine(ctx context.Context, seller uuid.UUID, pane Pane, p page.Request, t locale.Tag) (page.Page[ListingView], error) {
	statuses := domain.OnSale
	if pane == PaneSold {
		statuses = []domain.Status{domain.StatusSold}
	}
	rows, err := c.listings.ListBySeller(ctx, seller, statuses, nil, p)
	if err != nil {
		return page.Page[ListingView]{}, fmt.Errorf("list listings: %w", err)
	}
	items, err := c.views(ctx, rows, t)
	if err != nil {
		return page.Page[ListingView]{}, err
	}
	return page.Of(items, p, func(v ListingView) uuid.UUID { return v.ID }), nil
}

// OnSale lists the IDs of the seller's listings in a domain.OnSale status,
// newest first: at most domain.MaxOnSale, since the cap holds them.
func (c *Catalog) OnSale(ctx context.Context, seller uuid.UUID) ([]uuid.UUID, error) {
	ids, err := c.listings.IDsBySeller(ctx, seller, domain.OnSale)
	if err != nil {
		return nil, fmt.Errorf("list listings on sale: %w", err)
	}
	return ids, nil
}

func (c *Catalog) IDsBySlug(ctx context.Context, slugs []string) (map[string]uuid.UUID, error) {
	codes := make([]string, 0, len(slugs))
	for _, s := range slugs {
		if code := domain.CodeOf(s); code != "" {
			codes = append(codes, code)
		}
	}
	ids, err := c.listings.IDsByCode(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("find listing codes: %w", err)
	}
	out := make(map[string]uuid.UUID, len(ids))
	for _, s := range slugs {
		if id, ok := ids[domain.CodeOf(s)]; ok {
			out[s] = id
		}
	}
	return out, nil
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
			ID: l.ID(), Slug: l.Slug(), SellerID: l.SellerID(), Title: l.Title(), Price: l.Price(), AllowOffers: l.AllowOffers(),
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
