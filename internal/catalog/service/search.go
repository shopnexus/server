package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
)

// SearchCard is a listing as a search page shows it.
type SearchCard struct {
	ID            uuid.UUID        `json:"id"`
	Title         string           `json:"title"`
	Price         int64            `json:"price"`
	CoverThumbURL string           `json:"cover_thumb_url,omitempty"`
	Condition     domain.Condition `json:"condition"`
	AllowOffers   bool             `json:"allow_offers"`
	Commune       string           `json:"commune"`
	// DistanceKm is whole kilometres from the search's origin, 0 under 1
	// km; absent without an origin.
	DistanceKm *int `json:"distance_km,omitempty"`
}

func (c *Catalog) Search(ctx context.Context, s domain.Search) ([]SearchCard, error) {
	hits, err := c.searches.Search(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	ids := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	ls, err := c.listings.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	vs, err := c.views(ctx, ls)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]ListingView, len(vs))
	for i := range vs {
		byID[vs[i].ID] = vs[i]
	}
	out := make([]SearchCard, 0, len(hits))
	for _, h := range hits {
		v, ok := byID[h.ID]
		if !ok {
			continue // deleted between the index scan and the read
		}
		card := SearchCard{
			ID: v.ID, Title: v.Title, Price: v.Price, Condition: v.Condition, AllowOffers: v.AllowOffers,
			Commune: v.Location.Commune.DisplayName,
		}
		if len(v.Photos) > 0 {
			card.CoverThumbURL = v.Photos[0].ThumbURL
		}
		if o := s.Filters.Origin; o != nil {
			card.DistanceKm = new(int(domain.Distance(*o, h.Point)))
		}
		out = append(out, card)
	}
	return out, nil
}

func (c *Catalog) Counts(ctx context.Context, s domain.Search, by domain.CountField) ([]domain.CategoryCount, error) {
	counts, err := c.searches.Counts(ctx, s, by)
	if err != nil {
		return nil, fmt.Errorf("count listings: %w", err)
	}
	return counts, nil
}

func (c *Catalog) CommunePoint(ctx context.Context, code string) (domain.Point, bool, error) {
	places, err := c.regions.Places(ctx, []string{code})
	if err != nil {
		return domain.Point{}, false, fmt.Errorf("read commune: %w", err)
	}
	if len(places) != 1 {
		return domain.Point{}, false, nil
	}
	return domain.Point{Lat: places[0].Lat, Lng: places[0].Lng}, true, nil
}

func (c *Catalog) Place(ctx context.Context, name string) (domain.Point, bool, error) {
	points, err := c.regions.FindPlace(ctx, name)
	if err != nil {
		return domain.Point{}, false, fmt.Errorf("find place: %w", err)
	}
	if len(points) != 1 {
		return domain.Point{}, false, nil
	}
	return points[0], true, nil
}
