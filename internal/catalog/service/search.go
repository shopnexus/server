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
	s, err := c.withVectors(ctx, s)
	if err != nil {
		return nil, err
	}
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
	s, err := c.withVectors(ctx, s)
	if err != nil {
		return nil, err
	}
	counts, err := c.searches.Counts(ctx, s, by)
	if err != nil {
		return nil, fmt.Errorf("count listings: %w", err)
	}
	return counts, nil
}

func (c *Catalog) FindPlaces(ctx context.Context, names []string) (map[string]domain.Point, error) {
	found, err := c.regions.FindPlaces(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("find places: %w", err)
	}
	out := make(map[string]domain.Point, len(found))
	for name, points := range found {
		if len(points) == 1 {
			out[name] = points[0]
		}
	}
	return out, nil
}

// withVectors sets the vector versions of a hybrid search, and for a
// similar-listings search the source's vectors. A source with no vectors yet
// leaves its lists out, and BM25 and the boosts answer.
func (c *Catalog) withVectors(ctx context.Context, s domain.Search) (domain.Search, error) {
	if s.Hybrid() {
		s.Vectors.TextVersion, s.Vectors.PhotoVersion = c.textVersion, c.imageVersion
	}
	if s.Like != (uuid.UUID{}) {
		text, photo, err := c.vectors.Source(ctx, s.Like, c.textVersion, c.imageVersion)
		if err != nil {
			return s, fmt.Errorf("read the source's vectors: %w", err)
		}
		s.Vectors.Text, s.Vectors.Photo = text, photo
	}
	return s, nil
}

func (c *Catalog) Words(ctx context.Context, text string) ([]string, error) {
	words, err := c.searches.Words(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("split words: %w", err)
	}
	return words, nil
}
