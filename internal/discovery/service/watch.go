package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/repository"
)

// WatchInput is a search the person follows, as POST /v1/search/watches
// gives it. Near is turned into a commune and not kept.
type WatchInput struct {
	Query              string
	Category           string
	PriceMin, PriceMax int64
	Conditions         []catalogdomain.Condition
	OffersOnly         bool
	Near               Near
	RadiusKm           int
}

// WatchView is the SearchWatch schema.
type WatchView struct {
	ID        uuid.UUID    `json:"id"`
	Query     string       `json:"query"`
	Filters   WatchFilters `json:"filters"`
	Area      *WatchArea   `json:"area,omitempty"`
	RadiusKm  int          `json:"radius_km,omitempty"`
	NewCount  int          `json:"new_count"`
	Alert     bool         `json:"alert"`
	CreatedAt time.Time    `json:"created_at"`
}

// WatchFilters are the filters a watch keeps; each is absent when unset.
type WatchFilters struct {
	Category   string                    `json:"category,omitempty"`
	PriceMin   int64                     `json:"price_min,omitempty"`
	PriceMax   int64                     `json:"price_max,omitempty"`
	Conditions []catalogdomain.Condition `json:"conditions,omitempty"`
	OffersOnly bool                      `json:"offers_only,omitempty"`
}

// WatchArea is the commune a watch searches from.
type WatchArea struct {
	CommuneCode string `json:"commune_code"`
	Name        string `json:"name"`
}

// WatchList is the SearchWatchList schema.
type WatchList struct {
	Items []WatchView `json:"items"`
}

// Swept is what one sweep did: Found is false for a watch gone, which ends
// its chain, and Full when it read a whole batch, so more may wait.
type Swept struct {
	Found bool `json:"found"`
	Full  bool `json:"full"`
}

// WatchFacts is what a notice of a watch's new listings needs.
type WatchFacts struct {
	ID      uuid.UUID `json:"id"`
	Account uuid.UUID `json:"account"`
	Query   string    `json:"query"`
}

// Watch resolves the place to a commune, reads the query's phrases through
// the model, schedules the sweep of a new ID and stores the watch.
func (d *Discovery) Watch(ctx context.Context, account uuid.UUID, in WatchInput) (WatchView, bool, error) {
	q := strings.TrimSpace(in.Query)
	if in.PriceMin > 0 && in.PriceMax > 0 && in.PriceMin > in.PriceMax {
		return WatchView{}, false, domain.ErrInvalidPriceRange
	}
	commune := in.Near.Commune
	switch {
	case in.Near.Lat != 0 || in.Near.Lng != 0:
		if in.Near.Lat == 0 || in.Near.Lng == 0 || in.Near.Lat < -90 || in.Near.Lat > 90 || in.Near.Lng < -180 || in.Near.Lng > 180 {
			return WatchView{}, false, domain.ErrInvalidPosition
		}
		code, km, err := d.catalog.NearestCommune(ctx, catalogdomain.Point{Lat: in.Near.Lat, Lng: in.Near.Lng})
		if err != nil {
			return WatchView{}, false, fmt.Errorf("find the commune: %w", err)
		}
		if km > catalogdomain.MaxGPSKm {
			return WatchView{}, false, domain.ErrInvalidPosition
		}
		commune = code
	case commune != "":
		places, err := d.catalog.Places(ctx, []string{commune})
		if err != nil {
			return WatchView{}, false, fmt.Errorf("read the commune: %w", err)
		}
		if _, ok := places[commune]; !ok {
			return WatchView{}, false, domain.ErrInvalidPosition
		}
	default:
	}
	if in.RadiusKm > 0 && commune == "" {
		return WatchView{}, false, domain.ErrPositionRequired
	}
	now := d.now()
	categories, err := d.catalog.Categories(ctx)
	if err != nil {
		return WatchView{}, false, fmt.Errorf("read categories: %w", err)
	}
	u, _ := d.understand(ctx, q, categories, now)
	names := make(map[string]string, len(categories))
	for _, c := range categories {
		names[c.Code] = c.DisplayName
	}
	var phrases []string
	for _, p := range domain.Compile(u, q, domain.Vocabulary{Categories: names}, nil, now, nil).Phrases {
		if p.Weight > 0 && !slices.Contains(phrases, p.Text) {
			phrases = append(phrases, p.Text)
		}
	}
	conditions := make([]string, len(in.Conditions))
	for i, c := range in.Conditions {
		conditions[i] = string(c)
	}
	id := uuid.NewV7()
	if err = d.sweeps.Schedule(ctx, id); err != nil {
		return WatchView{}, false, fmt.Errorf("start the sweeps: %w", err)
	}
	w, created, err := d.watches.Put(ctx, repository.Watch{
		ID: id, Account: account, Query: q, Phrases: phrases, Category: in.Category, PriceMin: in.PriceMin,
		PriceMax: in.PriceMax, Conditions: conditions, OffersOnly: in.OffersOnly, Commune: commune,
		RadiusKm: in.RadiusKm, Alert: true, CreatedAt: now,
	})
	if err != nil {
		return WatchView{}, false, fmt.Errorf("store the watch: %w", err)
	}
	views, err := d.watchViews(ctx, []repository.Watch{w})
	if err != nil {
		return WatchView{}, false, err
	}
	return views[0], created, nil
}

func (d *Discovery) Watches(ctx context.Context, account uuid.UUID) (WatchList, error) {
	ws, err := d.watches.List(ctx, account)
	if err != nil {
		return WatchList{}, fmt.Errorf("list watches: %w", err)
	}
	views, err := d.watchViews(ctx, ws)
	if err != nil {
		return WatchList{}, err
	}
	return WatchList{Items: views}, nil
}

func (d *Discovery) SetWatchAlert(ctx context.Context, account, id uuid.UUID, on bool) error {
	found, err := d.watches.SetAlert(ctx, account, id, on)
	if err != nil {
		return fmt.Errorf("set watch alert: %w", err)
	}
	if !found {
		return domain.ErrWatchNotFound
	}
	return nil
}

func (d *Discovery) Unwatch(ctx context.Context, account, id uuid.UUID) error {
	if err := d.watches.Delete(ctx, account, id); err != nil {
		return fmt.Errorf("unwatch: %w", err)
	}
	return nil
}

func (d *Discovery) ListingArrived(ctx context.Context, listing uuid.UUID) error {
	if err := d.watches.Arrive(ctx, listing); err != nil {
		return fmt.Errorf("record the arrival: %w", err)
	}
	return nil
}

func (d *Discovery) WatchFacts(ctx context.Context, id uuid.UUID) (WatchFacts, bool, error) {
	w, found, err := d.watches.Get(ctx, id)
	if err != nil {
		return WatchFacts{}, false, fmt.Errorf("read the watch: %w", err)
	}
	if !found {
		return WatchFacts{}, false, nil
	}
	return WatchFacts{ID: w.ID, Account: w.Account, Query: w.Query}, true, nil
}

// Sweep reads the arrivals after the watch's mark, keeps those that match
// its filters and every word of one of its phrases, less the watcher's own,
// hidden and blocked listings, and moves the mark. A retried sweep after
// the move reads the next arrivals; one before it finds the mark where it
// was. Swept writes the event that tells the watcher.
func (d *Discovery) Sweep(ctx context.Context, id uuid.UUID) (Swept, error) {
	w, found, err := d.watches.Get(ctx, id)
	if err != nil {
		return Swept{}, fmt.Errorf("read the watch: %w", err)
	}
	if !found {
		return Swept{}, nil
	}
	arrivals, err := d.watches.Arrivals(ctx, w.LastArrival, d.arrivalLag, d.sweepBatch)
	if err != nil {
		return Swept{}, fmt.Errorf("read arrivals: %w", err)
	}
	if len(arrivals) == 0 {
		return Swept{Found: true}, nil
	}
	ids := make([]uuid.UUID, len(arrivals))
	for i, a := range arrivals {
		ids[i] = a.ListingID
	}
	hidden, sellers, err := d.exclusions(ctx, w.Account)
	if err != nil {
		return Swept{}, err
	}
	search := catalogdomain.Search{
		Filters: catalogdomain.Filters{PriceMin: w.PriceMin, PriceMax: w.PriceMax, OffersOnly: w.OffersOnly, IDs: ids},
		Sort:    catalogdomain.SortNewest, Limit: len(ids),
		ExcludeListings: hidden, ExcludeSellers: append(sellers, w.Account),
	}
	category(&search.Filters, w.Category)
	for _, c := range w.Conditions {
		search.Filters.Conditions = append(search.Filters.Conditions, catalogdomain.Condition(c))
	}
	if w.Commune != "" && w.RadiusKm > 0 {
		places, placeErr := d.catalog.Places(ctx, []string{w.Commune})
		if placeErr != nil {
			return Swept{}, fmt.Errorf("read the commune: %w", placeErr)
		}
		if loc, ok := places[w.Commune]; ok {
			search.Filters.Origin, search.Filters.RadiusKm = &catalogdomain.Point{Lat: loc.Lat, Lng: loc.Lng}, w.RadiusKm
		}
	}
	for _, p := range w.Phrases {
		search.Phrases = append(search.Phrases, catalogdomain.Phrase{Text: p, Weight: 1, AllWords: true})
	}
	if len(search.Phrases) == 0 {
		search.Phrases = []catalogdomain.Phrase{{Text: w.Query, Weight: 1, AllWords: true}}
	}
	cards, err := d.catalog.Search(ctx, search)
	if err != nil {
		return Swept{}, fmt.Errorf("match the arrivals: %w", err)
	}
	if _, err = d.watches.Swept(ctx, id, w.LastArrival, arrivals[len(arrivals)-1].Seq, len(cards), d.now()); err != nil {
		return Swept{}, fmt.Errorf("move the watch: %w", err)
	}
	return Swept{Found: true, Full: len(arrivals) == d.sweepBatch}, nil
}

// watchViews names the watches' communes in one read; Watch and Watches
// call it.
func (d *Discovery) watchViews(ctx context.Context, ws []repository.Watch) ([]WatchView, error) {
	var codes []string
	for i := range ws {
		if ws[i].Commune != "" {
			codes = append(codes, ws[i].Commune)
		}
	}
	places, err := d.catalog.Places(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("read communes: %w", err)
	}
	out := make([]WatchView, len(ws))
	for i := range ws {
		w := &ws[i]
		v := WatchView{
			ID: w.ID, Query: w.Query, RadiusKm: w.RadiusKm, NewCount: w.NewCount, Alert: w.Alert, CreatedAt: w.CreatedAt,
			Filters: WatchFilters{Category: w.Category, PriceMin: w.PriceMin, PriceMax: w.PriceMax, OffersOnly: w.OffersOnly},
		}
		for _, c := range w.Conditions {
			v.Filters.Conditions = append(v.Filters.Conditions, catalogdomain.Condition(c))
		}
		if loc, ok := places[w.Commune]; ok {
			v.Area = &WatchArea{CommuneCode: w.Commune, Name: loc.Commune.DisplayName}
		}
		out[i] = v
	}
	return out, nil
}
