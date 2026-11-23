package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/llm"
)

const (
	// SortNearby is the nearby feed, a sort search adds to catalog's: the
	// distance rings and freshness score the listings, with no q, photo or
	// like.
	SortNearby catalogdomain.SortOrder = "nearby"
	// nearRadiusKm bounds a nearest or nearby search that names no radius.
	nearRadiusKm = 50
)

// SearchInput is a search as the person shaped it.
type SearchInput struct {
	Query              string
	Category           string // a root or a subcategory code
	Province           string // a province code, empty for every province
	PriceMin, PriceMax int64
	Conditions         []catalogdomain.Condition
	RadiusKm           int
	OffersOnly         bool
	Sort               catalogdomain.SortOrder // empty picks relevance with a query, newest without; or SortNearby
	Off                []string
	Near               catalogdomain.Near
	Cursor             string
	Limit              int
	Photo, Like        uuid.UUID // zero for none
	// Watch is a watch of the viewer's, zero for none; it brings its query
	// and filters, and goes with Sort, Off, Cursor and Limit alone.
	Watch uuid.UUID
}

// SearchItem is a listing of a search page, and whether the viewer saved
// it.
type SearchItem struct {
	catalogsvc.SearchCard

	Saved bool `json:"saved"`
}

// SearchView is a page of a search.
type SearchView struct {
	// SearchID names the response, for the search events clients send back.
	SearchID   uuid.UUID                     `json:"search_id"`
	Items      []SearchItem                  `json:"items"`
	NextCursor string                        `json:"next_cursor,omitempty"`
	Chips      []domain.Chip                 `json:"chips,omitempty"`
	Counts     []catalogdomain.CategoryCount `json:"counts,omitempty"`
	// WatchID is the viewer's watch of the query, nil for none.
	WatchID *uuid.UUID `json:"watch_id,omitempty"`
}

// Search reads the query through the model, from the cache when it read the
// same query within domain.CacheTTL, and searches catalog with the person's
// filters and the boosts the model's answer resolves to. With vectors it
// fuses BM25, the query's text vector and its visual description by RRF;
// photo searches by a photo of the viewer's alone, and like by a listing.
// SortNearby browses by distance and age. A model that errs or does not
// answer within the wait leaves out the lists it feeds: q then searches its
// raw words, and the cursor keeps the next pages on them. A signed-in
// viewer's first page with q is kept in the history; a failed write is
// logged and the search still answers.
func (d *Discovery) Search(ctx context.Context, viewer *uuid.UUID, in SearchInput) (SearchView, error) {
	if in.Watch != uuid.Nil() {
		if in.Query != "" || in.Category != "" || in.Province != "" || in.PriceMin != 0 || in.PriceMax != 0 || len(in.Conditions) > 0 ||
			in.RadiusKm != 0 || in.OffersOnly || in.Near != (catalogdomain.Near{}) || in.Photo != uuid.Nil() || in.Like != uuid.Nil() {
			return SearchView{}, domain.ErrSearchConflict
		}
		if viewer == nil {
			return SearchView{}, authn.ErrUnauthenticated
		}
		w, found, err := d.watches.Get(ctx, in.Watch)
		if err != nil {
			return SearchView{}, fmt.Errorf("read the watch: %w", err)
		}
		if !found || w.Account != *viewer {
			return SearchView{}, domain.ErrWatchNotFound
		}
		in.Query, in.Category, in.PriceMin, in.PriceMax = w.Query, w.Category, w.PriceMin, w.PriceMax
		in.OffersOnly, in.RadiusKm, in.Near = w.OffersOnly, w.RadiusKm, catalogdomain.Near{Commune: w.Commune}
		in.Conditions = make([]catalogdomain.Condition, len(w.Conditions))
		for i, c := range w.Conditions {
			in.Conditions[i] = catalogdomain.Condition(c)
		}
	}
	q := strings.TrimSpace(in.Query)
	photo, like := in.Photo != uuid.Nil(), in.Like != uuid.Nil()
	if photo && (q != "" || like) {
		return SearchView{}, domain.ErrSearchConflict
	}
	if in.Sort == SortNearby && (q != "" || photo || like) {
		return SearchView{}, domain.ErrSearchConflict
	}
	if photo && viewer == nil {
		return SearchView{}, authn.ErrUnauthenticated
	}
	origin, err := d.catalog.Origin(ctx, in.Near)
	if err != nil {
		return SearchView{}, fmt.Errorf("read the position: %w", err)
	}
	if in.Province != "" {
		provinces, provErr := d.catalog.Provinces(ctx)
		if provErr != nil {
			return SearchView{}, fmt.Errorf("read the provinces: %w", provErr)
		}
		if !slices.ContainsFunc(provinces, func(p catalogdomain.Province) bool { return p.Code == in.Province }) {
			return SearchView{}, catalogdomain.ErrInvalidProvince
		}
	}
	offset, limit, raw, err := window(in.Cursor, in.Limit)
	if err != nil {
		return SearchView{}, err
	}
	if in.PriceMin > 0 && in.PriceMax > 0 && in.PriceMin > in.PriceMax {
		return SearchView{}, domain.ErrInvalidPriceRange
	}
	sort := in.Sort
	if sort == "" {
		sort = catalogdomain.SortNewest
		if q != "" || photo || like {
			sort = catalogdomain.SortRelevance
		}
	}
	near := sort == catalogdomain.SortNearest || sort == SortNearby
	radius := in.RadiusKm
	if (near || radius > 0) && origin == nil {
		return SearchView{}, domain.ErrPositionRequired
	}
	if near && radius == 0 {
		radius = nearRadiusKm
	}
	filters := catalogdomain.Filters{
		PriceMin: in.PriceMin, PriceMax: in.PriceMax, Conditions: in.Conditions, OffersOnly: in.OffersOnly,
		Origin: origin, RadiusKm: radius, Province: in.Province,
	}
	category(&filters, in.Category)
	search := catalogdomain.Search{Filters: filters, Sort: sort, Offset: offset, Limit: limit + 1}
	if sort == SortNearby {
		search.Sort, search.Boosts = catalogdomain.SortRelevance, domain.NearbyBoosts(*origin, d.now())
	}
	search.Vectors.TextMin, search.Vectors.PhotoMin = d.textMin, d.photoMin
	if viewer != nil {
		if search.ExcludeListings, search.ExcludeSellers, err = d.exclusions(ctx, *viewer); err != nil {
			return SearchView{}, err
		}
	}
	view := SearchView{SearchID: uuid.NewV7()}

	if photo {
		// Photo search needs the image model; with it off the search is
		// unavailable, as when the model fails.
		if d.image == nil {
			return SearchView{}, domain.ErrSearchUnavailable
		}
		now := d.now()
		up, upErr := d.media.QueryUpload(ctx, *viewer, in.Photo)
		if upErr != nil {
			return SearchView{}, fmt.Errorf("check the search photo: %w", upErr)
		}
		vec, found, cacheErr := d.photoVectors.Get(ctx, in.Photo, d.imageVersion, now)
		if cacheErr != nil {
			d.log.WarnContext(ctx, "read the cached photo vector", "err", cacheErr)
		}
		if !found {
			img, imgErr := d.media.QueryImage(ctx, *viewer, in.Photo)
			if imgErr != nil {
				return SearchView{}, fmt.Errorf("read the search photo: %w", imgErr)
			}
			vs, embedErr := d.image.Embed(ctx, []string{embed.ImageInput(img)})
			if embedErr != nil {
				d.log.WarnContext(ctx, "embed the search photo", "err", embedErr)
				return SearchView{}, domain.ErrSearchUnavailable
			}
			vec = vs[0]
			if putErr := d.photoVectors.Put(ctx, in.Photo, d.imageVersion, vec, up.ExpiresAt, now); putErr != nil {
				d.log.WarnContext(ctx, "cache the photo vector", "err", putErr)
			}
		}
		search.Vectors.Photo, search.Weights = vec, domain.FusePhoto
	}

	if like {
		src, ok, srcErr := d.catalog.Summary(ctx, in.Like)
		if srcErr != nil {
			return SearchView{}, fmt.Errorf("read the source listing: %w", srcErr)
		}
		if !ok || !slices.Contains(catalogdomain.Public, src.Status) {
			return SearchView{}, catalogdomain.ErrNotFound
		}
		search.Phrases, search.Boosts = domain.LikeTerms(src.Title, src.CategoryCode, src.Price)
		search.Like, search.Weights = in.Like, domain.FuseLike
		if viewer != nil {
			search.ExcludeSellers = append(search.ExcludeSellers, *viewer)
		}
	}

	if q != "" {
		now := d.now()
		categories, catErr := d.catalog.Categories(ctx)
		if catErr != nil {
			return SearchView{}, fmt.Errorf("read categories: %w", catErr)
		}
		// The raw words embed while the model reads them: the vector is ready
		// when the model does not answer in time, or rewrites the query to
		// the same words.
		var (
			rawVector []float32
			rawEmbed  sync.WaitGroup
		)
		rctx, rcancel := context.WithTimeout(ctx, d.modelTimeout)
		defer rcancel()
		if !like && d.text != nil {
			rawEmbed.Go(func() { rawVector = d.queryVector(rctx, d.text, d.textVersion, q) })
		}
		// A page after one that searched the raw words searches them too, so
		// an answer cached in between does not reorder the pages.
		var u domain.Understanding
		understood := false
		if !raw {
			u, understood = d.understand(ctx, q, categories, now, d.modelWait)
		}
		raw = !understood
		names := make(map[string]string, len(categories))
		for _, c := range categories {
			names[c.Code] = c.DisplayName
		}
		vocab := domain.Vocabulary{Categories: names}
		if placeNames := domain.PlaceNames(u); len(placeNames) > 0 {
			var placeErr error
			if vocab.Places, placeErr = d.catalog.FindPlaces(ctx, placeNames); placeErr != nil {
				d.log.WarnContext(ctx, "resolve the places", "err", placeErr)
			}
		}
		c := domain.Compile(u, q, vocab, origin, now, in.Off)
		search.Phrases = append(search.Phrases, c.Phrases...)
		search.Boosts = append(search.Boosts, c.Boosts...)
		view.Chips = c.Chips
		if !like {
			// The text vector reads the rewrite, or the raw words when the
			// model did not answer or kept them; the photo list needs the
			// English description, which only the model gives.
			rewrite := understood && strings.TrimSpace(u.Rewritten) != "" && domain.QueryKey(u.Rewritten) != domain.QueryKey(q)
			// The embeds run at once under the model timeout, so a hung alias
			// costs the search that much and its list is left out.
			ectx, cancel := context.WithTimeout(ctx, d.modelTimeout)
			var wg sync.WaitGroup
			if d.text != nil && rewrite {
				wg.Go(func() { search.Vectors.Text = d.queryVector(ectx, d.text, d.textVersion, u.Rewritten) })
			}
			if d.image != nil && understood && strings.TrimSpace(u.Visual) != "" {
				// A text sits far from any photo; the look has its own floor.
				search.Vectors.PhotoMin = d.lookMin
				wg.Go(func() { search.Vectors.Photo = d.queryVector(ectx, d.image, d.imageVersion, u.Visual) })
			}
			wg.Wait()
			rawEmbed.Wait()
			cancel()
			if !rewrite {
				search.Vectors.Text = rawVector
			}
			if search.Vectors.Text != nil || search.Vectors.Photo != nil {
				search.Weights = domain.FuseQuery
			}
		}
		d.log.InfoContext(ctx, "search terms", "search_id", view.SearchID, "query", q, "asked", c.Asked, "ran", c.Ran, "lists", search.Weights)
	}

	// The counts run beside the search: a hybrid count reads its lists at
	// full depth and is the slower of the two.
	var countErr error
	var wg sync.WaitGroup
	if filters.CategoryRoot != "" {
		wg.Go(func() { view.Counts, countErr = d.catalog.Counts(ctx, search) })
	}
	cards, err := d.catalog.Search(ctx, search)
	wg.Wait()
	if err != nil {
		return SearchView{}, fmt.Errorf("search listings: %w", err)
	}
	if countErr != nil {
		return SearchView{}, fmt.Errorf("count categories: %w", countErr)
	}
	cards, view.NextCursor = cut(cards, offset, limit, raw)
	if view.Items, err = d.withSaved(ctx, viewer, cards); err != nil {
		return SearchView{}, err
	}
	if viewer != nil && q != "" && in.Cursor == "" {
		entry := repository.HistoryEntry{Query: q, SearchedAt: d.now()}
		for _, c := range view.Chips {
			if c.Kind == domain.AttrCategory {
				entry.Category, entry.CategoryCode = c.Label, c.Code
				break
			}
		}
		if len(view.Items) > 0 {
			entry.PhotoURL = view.Items[0].CoverThumbURL
		}
		if err = d.history.Remember(ctx, *viewer, entry); err != nil {
			d.log.WarnContext(ctx, "remember the search", "err", err)
		}
	}
	if viewer != nil && q != "" {
		id, found, keyErr := d.watches.ByKey(ctx, *viewer, domain.HistoryKey(q))
		switch {
		case keyErr != nil:
			d.log.WarnContext(ctx, "read the watch of the query", "err", keyErr)
		case found:
			view.WatchID = &id
		default:
		}
	}
	if in.Watch != uuid.Nil() && in.Cursor == "" {
		if err = d.watches.Seen(ctx, *viewer, in.Watch); err != nil {
			d.log.WarnContext(ctx, "see the watch", "err", err)
		}
	}
	return view, nil
}

// understand is the model's reading of q, from the cache when the same query
// was read within domain.CacheTTL, and whether the model answered. The call
// runs apart from ctx and caches its answer; understand waits for it at most
// wait, or until it ends when wait is zero, and gives the zero reading when
// it errs or is still running. Searches of one query share one call. Search
// and Watch call it.
func (d *Discovery) understand(
	ctx context.Context, q string, categories []catalogdomain.Category, now time.Time, wait time.Duration,
) (domain.Understanding, bool) {
	var u domain.Understanding
	key := domain.QueryKey(q)
	cached, found, err := d.understandings.Get(ctx, key, domain.PromptVersion, now.Add(-domain.CacheTTL))
	switch {
	case err != nil:
		d.log.WarnContext(ctx, "read the cached understanding", "err", err)
	case found && json.Unmarshal(cached, &u) == nil:
		return u, true
	default:
	}
	read := d.reads.DoChan(key, func() (any, error) {
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.modelTimeout)
		defer cancel()
		var answer domain.Understanding
		err := d.llm.Complete(mctx, llm.Request{
			System: domain.SystemPrompt(categories, now), User: q, SchemaName: "search_understanding", Schema: domain.Schema(),
		}, &answer)
		if err != nil {
			d.log.WarnContext(mctx, "the model did not read the query", "err", err)
			return nil, err
		}
		b, err := json.Marshal(answer)
		if err == nil {
			err = d.understandings.Put(mctx, key, domain.PromptVersion, b, now, now.Add(-domain.CacheTTL))
		}
		if err != nil {
			d.log.WarnContext(mctx, "cache the understanding", "err", err)
		}
		return answer, nil
	})
	var timer <-chan time.Time
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		timer = t.C
	}
	select {
	case r := <-read:
		if r.Err != nil {
			return domain.Understanding{}, false
		}
		u, _ = r.Val.(domain.Understanding)
		return u, true
	case <-timer:
		d.log.InfoContext(ctx, "searched before the model answered", "wait", wait)
		return domain.Understanding{}, false
	case <-ctx.Done():
		return domain.Understanding{}, false
	}
}

// category sets f's root or subcategory from a code: a subcategory has a
// dot. Search and Sweep call it.
func category(f *catalogdomain.Filters, code string) {
	if strings.Contains(code, ".") {
		f.CategoryCode = code
	} else {
		f.CategoryRoot = code
	}
}

// queryVector embeds a search text with m, from the cache when the same text
// was embedded under version and the row is still kept. A model that fails
// gives nil, and the search runs without that list.
func (d *Discovery) queryVector(ctx context.Context, m embed.Model, version, text string) []float32 {
	now := d.now()
	v, found, err := d.queryVectors.Get(ctx, version, text, now)
	if err != nil {
		d.log.WarnContext(ctx, "read the cached query vector", "err", err)
	}
	if found {
		return v
	}
	vs, err := m.Embed(ctx, []string{text})
	if err != nil {
		d.log.WarnContext(ctx, "embed the query", "version", version, "err", err)
		return nil
	}
	if err = d.queryVectors.Put(ctx, version, text, vs[0], now); err != nil {
		d.log.WarnContext(ctx, "cache the query vector", "err", err)
	}
	d.startVectorSweep(ctx, now)
	return vs[0]
}

// startVectorSweep starts the day's sweep of the text vectors once per
// process and day, after a write; Restate runs one per day.
func (d *Discovery) startVectorSweep(ctx context.Context, now time.Time) {
	day := now.UTC().Format(time.DateOnly)
	if d.sweeps == nil || d.vectorDay.Swap(day) == day {
		return
	}
	if err := d.sweeps.SweepVectors(ctx, day); err != nil {
		d.vectorDay.Store("")
		d.log.WarnContext(ctx, "start the vector sweep", "err", err)
	}
}

// SweepVectors deletes the text vectors that fell out of use and the least
// used past domain.VectorCacheMax.
func (d *Discovery) SweepVectors(ctx context.Context) (int64, error) {
	n, err := d.queryVectors.Sweep(ctx, d.now(), domain.VectorKeepUnused, domain.VectorKeepUsed, domain.VectorCacheMax)
	if err != nil {
		return 0, err
	}
	d.log.InfoContext(ctx, "swept the text vectors", "deleted", n)
	return n, nil
}

// rawMark ends a cursor whose search ran on the raw words.
const rawMark = "r"

// window reads a cursor, the offset of the next page and whether the pages
// before searched the raw words, and clips the limit so a search stops at
// catalogdomain.MaxResults.
func window(cursor string, limit int) (offset, size int, raw bool, err error) {
	if cursor != "" {
		cursor, raw = strings.CutSuffix(cursor, rawMark)
		offset, err = strconv.Atoi(cursor)
		if err != nil || offset < 0 || offset >= catalogdomain.MaxResults {
			return 0, 0, false, domain.ErrInvalidCursor
		}
	}
	size = limit
	if size <= 0 {
		size = page.DefaultLimit
	}
	return offset, min(size, catalogdomain.MaxResults-offset), raw, nil
}

// cut drops the extra card a page read to learn whether another follows,
// and gives the next page's cursor, marked when the page searched the raw
// words.
func cut(cards []catalogsvc.SearchCard, offset, limit int, raw bool) (kept []catalogsvc.SearchCard, next string) {
	if len(cards) <= limit {
		return cards, ""
	}
	if offset+limit >= catalogdomain.MaxResults {
		return cards[:limit], ""
	}
	next = strconv.Itoa(offset + limit)
	if raw {
		next += rawMark
	}
	return cards[:limit], next
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
