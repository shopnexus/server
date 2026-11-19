package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
)

// SearchCard is a listing as a search page and the seller view show it.
type SearchCard struct {
	ID            uuid.UUID        `json:"id"`
	Slug          string           `json:"slug"`
	SellerID      uuid.UUID        `json:"seller_id"`
	Title         string           `json:"title"`
	Price         int64            `json:"price"`
	CoverThumbURL string           `json:"cover_thumb_url,omitempty"`
	CategoryCode  string           `json:"category_code"`
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
	byID := make(map[uuid.UUID]*domain.Listing, len(ls))
	for i := range ls {
		byID[ls[i].ID()] = &ls[i]
	}
	inOrder := make([]domain.Listing, 0, len(hits))
	for _, h := range hits {
		if l, ok := byID[h.ID]; ok { // a listing deleted between the index scan and the read is gone
			inOrder = append(inOrder, *l)
		}
	}
	return c.cards(ctx, inOrder, s.Filters.Origin)
}

// cards are ls as cards, in order, each measured from origin when it is not
// nil; one query reads the communes of all of them.
func (c *Catalog) cards(ctx context.Context, ls []domain.Listing, origin *domain.Point) ([]SearchCard, error) {
	codes := make([]string, len(ls))
	for i := range ls {
		codes[i] = ls[i].CommuneCode()
	}
	places, err := c.Places(ctx, codes)
	if err != nil {
		return nil, err
	}
	out := make([]SearchCard, len(ls))
	for i := range ls {
		l := &ls[i]
		out[i] = SearchCard{
			ID: l.ID(), Slug: l.Slug(), SellerID: l.SellerID(), Title: l.Title(), Price: l.Price(), CategoryCode: l.CategoryCode(),
			Condition: l.Condition(), AllowOffers: l.AllowOffers(), Commune: places[l.CommuneCode()].Commune.DisplayName,
		}
		if ps := l.Photos(); len(ps) > 0 && ps[0].Keys.Photo != "" {
			out[i].CoverThumbURL = c.media.URL(ps[0].Keys.Thumb)
		}
		if origin != nil {
			out[i].DistanceKm = new(domain.DistanceKm(*origin, l.Point()))
		}
	}
	return out, nil
}

func (c *Catalog) Counts(ctx context.Context, s domain.Search) ([]domain.CategoryCount, error) {
	s, err := c.withVectors(ctx, s)
	if err != nil {
		return nil, err
	}
	counts, err := c.searches.Counts(ctx, s)
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
