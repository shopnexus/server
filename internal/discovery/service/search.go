package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/provider/llm"
)

const (
	// nearestRadiusKm bounds a nearest-first search that names no radius.
	nearestRadiusKm = 50
	// nearbyRadiusKm bounds the nearby feed.
	nearbyRadiusKm = 50
)

// Near is where the person searches from: a GPS point, or a commune. It is
// used for this request only, never stored or logged.
type Near struct {
	Lat, Lng float64
	Commune  string
}

// SearchInput is a search as the person shaped it.
type SearchInput struct {
	Query              string
	Category           string // a root or a subcategory code
	PriceMin, PriceMax int64
	Conditions         []catalogdomain.Condition
	RadiusKm           int
	OffersOnly         bool
	Sort               catalogdomain.SortOrder // empty picks relevance with a query, newest without
	Off                []string
	Near               Near
	Cursor             string
	Limit              int
}

// SearchItem is a listing of a search page, and whether the viewer saved
// it.
type SearchItem struct {
	catalogsvc.SearchCard

	Saved bool `json:"saved"`
}

// SearchView is a page of a search or of the nearby feed.
type SearchView struct {
	Items      []SearchItem                  `json:"items"`
	NextCursor string                        `json:"next_cursor,omitempty"`
	Understood string                        `json:"understood,omitempty"`
	Chips      []domain.Chip                 `json:"chips,omitempty"`
	Counts     []catalogdomain.CategoryCount `json:"counts,omitempty"`
}

// Search reads the query through the model, from the cache when it read the
// same query within domain.CacheTTL, and searches catalog with the person's
// filters and the boosts the model's answer resolves to. A model that errs
// or takes longer than the timeout leaves the raw words to search alone.
func (d *Discovery) Search(ctx context.Context, viewer *uuid.UUID, in SearchInput) (SearchView, error) {
	origin, err := d.origin(ctx, in.Near)
	if err != nil {
		return SearchView{}, err
	}
	offset, limit, err := window(in.Cursor, in.Limit)
	if err != nil {
		return SearchView{}, err
	}
	if in.PriceMin > 0 && in.PriceMax > 0 && in.PriceMin > in.PriceMax {
		return SearchView{}, domain.ErrInvalidPriceRange
	}
	q := strings.TrimSpace(in.Query)
	sort := in.Sort
	if sort == "" {
		sort = catalogdomain.SortNewest
		if q != "" {
			sort = catalogdomain.SortRelevance
		}
	}
	radius := in.RadiusKm
	if (sort == catalogdomain.SortNearest || radius > 0) && origin == nil {
		return SearchView{}, domain.ErrPositionRequired
	}
	if sort == catalogdomain.SortNearest && radius == 0 {
		radius = nearestRadiusKm
	}
	filters := catalogdomain.Filters{
		PriceMin: in.PriceMin, PriceMax: in.PriceMax, Conditions: in.Conditions, OffersOnly: in.OffersOnly,
		Origin: origin, RadiusKm: radius,
	}
	if strings.Contains(in.Category, ".") {
		filters.CategoryCode = in.Category
	} else {
		filters.CategoryRoot = in.Category
	}
	search := catalogdomain.Search{Filters: filters, Sort: sort, Offset: offset, Limit: limit + 1}
	var view SearchView

	if q != "" {
		now := d.now()
		categories, catErr := d.catalog.Categories(ctx)
		if catErr != nil {
			return SearchView{}, fmt.Errorf("read categories: %w", catErr)
		}
		var u domain.Understanding
		understood := false
		key := domain.QueryKey(q)
		cached, found, cacheErr := d.understandings.Get(ctx, key, domain.PromptVersion, now.Add(-domain.CacheTTL))
		switch {
		case cacheErr != nil:
			d.log.WarnContext(ctx, "read the cached understanding", "err", cacheErr)
		case found:
			understood = json.Unmarshal(cached, &u) == nil
		default:
		}
		if !understood {
			mctx, cancel := context.WithTimeout(ctx, d.modelTimeout)
			modelErr := d.llm.Complete(mctx, llm.Request{
				System: domain.SystemPrompt(categories, now), User: q, SchemaName: "search_understanding", Schema: domain.Schema(),
			}, &u)
			cancel()
			if modelErr != nil {
				d.log.WarnContext(ctx, "the model did not read the query", "err", modelErr)
				u = domain.Understanding{}
			} else {
				understood = true
				answer, encErr := json.Marshal(u)
				if encErr == nil {
					encErr = d.understandings.Put(ctx, key, domain.PromptVersion, answer, now, now.Add(-domain.CacheTTL))
				}
				if encErr != nil {
					d.log.WarnContext(ctx, "cache the understanding", "err", encErr)
				}
			}
		}
		names := make(map[string]string, len(categories))
		for _, c := range categories {
			names[c.Code] = c.DisplayName
		}
		vocab := domain.Vocabulary{Categories: names, Place: func(name string) (catalogdomain.Point, bool) {
			p, ok, placeErr := d.catalog.Place(ctx, name)
			if placeErr != nil {
				d.log.WarnContext(ctx, "resolve a place", "err", placeErr)
			}
			return p, ok
		}}
		c := domain.Compile(u, q, vocab, origin, now, in.Off)
		d.log.InfoContext(ctx, "search terms", "query", q, "asked", c.Asked, "ran", c.Ran)
		search.Phrases, search.Boosts, view.Chips = c.Phrases, c.Boosts, c.Chips
		if understood {
			view.Understood = u.Understood
		}
	}

	cards, err := d.catalog.Search(ctx, search)
	if err != nil {
		return SearchView{}, fmt.Errorf("search listings: %w", err)
	}
	cards, view.NextCursor = cut(cards, offset, limit)
	if filters.CategoryRoot != "" {
		if view.Counts, err = d.catalog.Counts(ctx, search, catalogdomain.CountCode); err != nil {
			return SearchView{}, fmt.Errorf("count categories: %w", err)
		}
	}
	if view.Items, err = d.withSaved(ctx, viewer, cards); err != nil {
		return SearchView{}, err
	}
	return view, nil
}

// Nearby reads the nearby feed: active listings within nearbyRadiusKm,
// scored by distance and age alone.
func (d *Discovery) Nearby(ctx context.Context, viewer *uuid.UUID, near Near, cursor string, limit int) (SearchView, error) {
	origin, err := d.origin(ctx, near)
	if err != nil {
		return SearchView{}, err
	}
	if origin == nil {
		return SearchView{}, domain.ErrPositionRequired
	}
	offset, limit, err := window(cursor, limit)
	if err != nil {
		return SearchView{}, err
	}
	cards, err := d.catalog.Search(ctx, catalogdomain.Search{
		Filters: catalogdomain.Filters{Origin: origin, RadiusKm: nearbyRadiusKm},
		Boosts:  domain.NearbyBoosts(*origin, d.now()), Sort: catalogdomain.SortRelevance,
		Offset: offset, Limit: limit + 1,
	})
	if err != nil {
		return SearchView{}, fmt.Errorf("read the nearby feed: %w", err)
	}
	var view SearchView
	cards, view.NextCursor = cut(cards, offset, limit)
	if view.Items, err = d.withSaved(ctx, viewer, cards); err != nil {
		return SearchView{}, err
	}
	return view, nil
}

// origin is the point near names, or nil when it names none.
func (d *Discovery) origin(ctx context.Context, near Near) (*catalogdomain.Point, error) {
	switch {
	case near.Lat != 0 || near.Lng != 0:
		if near.Lat == 0 || near.Lng == 0 || near.Lat < -90 || near.Lat > 90 || near.Lng < -180 || near.Lng > 180 {
			return nil, domain.ErrInvalidPosition
		}
		return &catalogdomain.Point{Lat: near.Lat, Lng: near.Lng}, nil
	case near.Commune != "":
		p, ok, err := d.catalog.CommunePoint(ctx, near.Commune)
		if err != nil {
			return nil, fmt.Errorf("read the commune: %w", err)
		}
		if !ok {
			return nil, domain.ErrInvalidPosition
		}
		return &p, nil
	default:
		return nil, nil //nolint:nilnil // no position is a valid answer, not a missing one
	}
}

// window reads a cursor, the offset of the next page, and clips the limit
// so a search stops at catalogdomain.MaxResults.
func window(cursor string, limit int) (offset, size int, err error) {
	if cursor != "" {
		offset, err = strconv.Atoi(cursor)
		if err != nil || offset < 0 || offset >= catalogdomain.MaxResults {
			return 0, 0, domain.ErrInvalidCursor
		}
	}
	size = limit
	if size <= 0 {
		size = page.DefaultLimit
	}
	return offset, min(size, catalogdomain.MaxResults-offset), nil
}

// cut drops the extra card a page read to learn whether another follows,
// and gives the next page's cursor.
func cut(cards []catalogsvc.SearchCard, offset, limit int) (kept []catalogsvc.SearchCard, next string) {
	if len(cards) <= limit {
		return cards, ""
	}
	if offset+limit >= catalogdomain.MaxResults {
		return cards[:limit], ""
	}
	return cards[:limit], strconv.Itoa(offset + limit)
}

// withSaved marks the cards the viewer saved, in one read.
func (d *Discovery) withSaved(ctx context.Context, viewer *uuid.UUID, cards []catalogsvc.SearchCard) ([]SearchItem, error) {
	out := make([]SearchItem, len(cards))
	for i := range cards {
		out[i] = SearchItem{SearchCard: cards[i]}
	}
	if viewer == nil || len(cards) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(cards))
	for i := range cards {
		ids[i] = cards[i].ID
	}
	saved, err := d.saves.SavedAmong(ctx, *viewer, ids)
	if err != nil {
		return nil, fmt.Errorf("read saves: %w", err)
	}
	for i := range out {
		out[i].Saved = saved[out[i].ID]
	}
	return out, nil
}
