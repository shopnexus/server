package service

import (
	"context"
	"fmt"
	"strings"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
)

// WatcherQuery is a listing not yet posted: its category, condition,
// point, and the text its words are matched in (title, description,
// specs).
type WatcherQuery struct {
	CategoryCode string
	Condition    catalogdomain.Condition
	Point        catalogdomain.Point
	Text         string
}

// Watcher is a matching watch's price and offers filters; 0 is no bound.
// Nothing in it names the watcher.
type Watcher struct {
	PriceMin   int64 `json:"price_min,omitempty"`
	PriceMax   int64 `json:"price_max,omitempty"`
	OffersOnly bool  `json:"offers_only"`
}

func (d *Discovery) Watchers(ctx context.Context, seller uuid.UUID, q WatcherQuery) ([]Watcher, error) {
	blocks, err := d.accounts.BlockedWith(ctx, seller)
	if err != nil {
		return nil, fmt.Errorf("read blocks: %w", err)
	}
	root, _, _ := strings.Cut(q.CategoryCode, ".")
	candidates, err := d.watches.Candidates(ctx, repository.CandidateFilter{
		CategoryCode: q.CategoryCode, CategoryRoot: root, Condition: string(q.Condition),
		Exclude: append([]uuid.UUID{seller}, blocks...), Limit: domain.MaxWatcherCandidates,
	})
	if err != nil {
		return nil, fmt.Errorf("read watch candidates: %w", err)
	}
	out := []Watcher{}
	for _, w := range candidates {
		if len(out) == domain.MaxWatchers {
			break
		}
		// The sweep measures a watch's area as a box around its commune.
		if w.RadiusKm > 0 {
			lo, hi := catalogdomain.Box(w.Point, float64(w.RadiusKm))
			if q.Point.Lat < lo.Lat || q.Point.Lat > hi.Lat || q.Point.Lng < lo.Lng || q.Point.Lng > hi.Lng {
				continue
			}
		}
		phrases := w.Phrases
		if len(phrases) == 0 {
			phrases = []string{w.Query}
		}
		for _, p := range phrases {
			if catalogdomain.HoldsAllWords(p, q.Text) {
				out = append(out, Watcher{PriceMin: w.PriceMin, PriceMax: w.PriceMax, OffersOnly: w.OffersOnly})
				break
			}
		}
	}
	return out, nil
}
