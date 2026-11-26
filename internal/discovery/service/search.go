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
	"github.com/shopnexus/server/internal/platform/locale"
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
	// Locale is the language of the chips; empty reads as the default.
	Locale locale.Tag
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
	SearchID   uuid.UUID     `json:"search_id"`
	Items      []SearchItem  `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
	Chips      []domain.Chip `json:"chips,omitempty"`
	// PreviewChips are a guest's first page's chips the model read for the
	// query when someone signed in searched it; the search did not apply them.
	PreviewChips []domain.Chip `json:"preview_chips,omitempty"`
	// WatchID is the viewer's watch of the query, nil for none.
	WatchID *uuid.UUID `json:"watch_id,omitempty"`
}

// Search reads the query through the model, from the cache when it read the
// same query within domain.CacheTTL, and searches catalog with the person's
// filters and the boosts the model's answer resolves to. With vectors it
// fuses BM25, the query's text vector and its visual description by RRF;
// photo searches by a photo of the viewer's alone, and like by a listing.
// SortNearby browses by distance and age. A guest's q, and a q the model
// errs on or outlasts the timeout for, searches its raw words without the
// lists the model feeds, and the cursor keeps the next pages on them; a
// guest's q embeds nothing either. A signed-in
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
		w, found, err := d.d.Watches.Get(ctx, in.Watch)
		if err != nil {
			return SearchView{}, fmt.Errorf("read the watch: %w", err)
		}
		if !found || w.Account != *viewer {
			return SearchView{}, domain.ErrWatchNotFound
		}
		in.Query, in.Category, in.PriceMin, in.PriceMax = w.Query, w.Category, w.PriceMin, w.PriceMax
		in.OffersOnly, in.RadiusKm, in.Near = w.OffersOnly, w.RadiusKm, catalogdomain.Near{Commune: w.Commune}
		in.Conditions = w.Conditions
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
	origin, err := d.d.Catalog.Origin(ctx, in.Near)
	if err != nil {
		return SearchView{}, fmt.Errorf("read the position: %w", err)
	}
	if in.Province != "" {
		provinces, provErr := d.d.Catalog.Provinces(ctx)
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
		search.Sort, search.Boosts = catalogdomain.SortRelevance, domain.NearbyBoosts(*origin, d.d.Now())
	}
	search.Vectors.TextMin, search.Vectors.PhotoMin = d.d.TextMin, d.d.PhotoMin
	if viewer != nil {
		if search.ExcludeListings, search.ExcludeSellers, err = d.exclusions(ctx, *viewer); err != nil {
			return SearchView{}, err
		}
	}
	view := SearchView{SearchID: uuid.NewV7()}

	if photo {
		// Photo search needs the image model; with it off the search is
		// unavailable, as when the model fails.
		if d.d.Image == nil {
			return SearchView{}, domain.ErrSearchUnavailable
		}
		now := d.d.Now()
		up, upErr := d.d.Media.QueryUpload(ctx, *viewer, in.Photo)
		if upErr != nil {
			return SearchView{}, fmt.Errorf("check the search photo: %w", upErr)
		}
		vec, found, cacheErr := d.d.PhotoVectors.Get(ctx, in.Photo, d.d.ImageVersion, now)
		if cacheErr != nil {
			d.d.Log.WarnContext(ctx, "read the cached photo vector", "err", cacheErr)
		}
		if !found {
			img, imgErr := d.d.Media.QueryImage(ctx, *viewer, in.Photo)
			if imgErr != nil {
				return SearchView{}, fmt.Errorf("read the search photo: %w", imgErr)
			}
			vs, embedErr := d.d.Image.Embed(ctx, []string{embed.ImageInput(img)})
			if embedErr != nil {
				d.d.Log.WarnContext(ctx, "embed the search photo", "err", embedErr)
				return SearchView{}, domain.ErrSearchUnavailable
			}
			vec = vs[0]
			if putErr := d.d.PhotoVectors.Put(ctx, in.Photo, d.d.ImageVersion, vec, up.ExpiresAt, now); putErr != nil {
				d.d.Log.WarnContext(ctx, "cache the photo vector", "err", putErr)
			}
		}
		search.Vectors.Photo, search.Weights = vec, domain.FusePhoto
	}

	if like {
		src, ok, srcErr := d.d.Catalog.Summary(ctx, in.Like)
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

	// names are the categories' Vietnamese names, by code.
	var names map[string]string
	if q != "" {
		now := d.d.Now()
		categories, catErr := d.d.Catalog.Categories(ctx, locale.Vietnamese)
		if catErr != nil {
			return SearchView{}, fmt.Errorf("read categories: %w", catErr)
		}
		// A guest searches the raw words; the model reads signed-in people's
		// queries. A page after one that searched the raw words searches them
		// too, so an answer cached in between does not reorder the pages. A
		// guest's first page shows, unapplied, the chips of a cached answer.
		var u, preview domain.Understanding
		understood, previewed := false, false
		switch {
		case viewer != nil && !raw:
			u, understood = d.understand(ctx, *viewer, q, categories, now)
		case viewer == nil && in.Cursor == "" && !like:
			preview, previewed = d.cached(ctx, q, now)
		default:
		}
		raw = !understood
		names = make(map[string]string, len(categories))
		for _, c := range categories {
			names[c.Code] = c.DisplayName
		}
		vocab := domain.Vocabulary{Categories: names, Locale: in.Locale}
		if in.Locale != "" && in.Locale != locale.Vietnamese {
			named, nameErr := d.d.Catalog.Categories(ctx, in.Locale)
			if nameErr != nil {
				return SearchView{}, fmt.Errorf("read category names: %w", nameErr)
			}
			vocab.Labels = make(map[string]string, len(named))
			for _, c := range named {
				vocab.Labels[c.Code] = c.DisplayName
			}
		}
		if placeNames := append(domain.PlaceNames(u), domain.PlaceNames(preview)...); len(placeNames) > 0 {
			var placeErr error
			if vocab.Places, placeErr = d.d.Catalog.FindPlaces(ctx, placeNames); placeErr != nil {
				d.d.Log.WarnContext(ctx, "resolve the places", "err", placeErr)
			}
		}
		c := domain.Compile(u, q, vocab, origin, now, in.Off)
		if previewed {
			view.PreviewChips = domain.Compile(preview, q, vocab, origin, now, nil).Chips
		}
		// The raw words, a guest's or what the model did not read, are often
		// typed without diacritics; the model's rewrite has them.
		if !understood {
			for i := range c.Phrases {
				c.Phrases[i].Folded = true
			}
		}
		search.Phrases = append(search.Phrases, c.Phrases...)
		search.Boosts = append(search.Boosts, c.Boosts...)
		view.Chips = c.Chips
		// A guest's words search through BM25 alone: their vector, typed
		// without diacritics, brought listings that share no word.
		if !like && viewer != nil {
			// The text vector reads the rewrite, or the raw words when the
			// model did not answer; the photo list needs the English
			// description, which only the model gives.
			text := q
			if understood && strings.TrimSpace(u.Rewritten) != "" {
				text = u.Rewritten
			}
			// Both embeds run at once under the model timeout, so a hung
			// alias costs the search that much and its list is left out.
			ectx, cancel := context.WithTimeout(ctx, d.d.ModelTimeout)
			var wg sync.WaitGroup
			if d.d.Text != nil {
				wg.Go(func() { search.Vectors.Text = d.queryVector(ectx, d.d.Text, d.d.TextVersion, text) })
			}
			if d.d.Image != nil && understood && strings.TrimSpace(u.Visual) != "" {
				// A text sits far from any photo; the look has its own floor.
				search.Vectors.PhotoMin = d.d.LookMin
				wg.Go(func() { search.Vectors.Photo = d.queryVector(ectx, d.d.Image, d.d.ImageVersion, u.Visual) })
			}
			wg.Wait()
			cancel()
			if search.Vectors.Text != nil || search.Vectors.Photo != nil {
				search.Weights = domain.FuseQuery
			}
		}
		d.d.Log.InfoContext(ctx, "search terms", "search_id", view.SearchID, "query", q, "asked", c.Asked, "ran", c.Ran, "lists", search.Weights)
	}

	cards, err := d.d.Catalog.Search(ctx, search)
	if err != nil {
		return SearchView{}, fmt.Errorf("search listings: %w", err)
	}
	cards, view.NextCursor = cut(cards, offset, limit, raw)
	if view.Items, err = d.withSaved(ctx, viewer, cards); err != nil {
		return SearchView{}, err
	}
	if viewer != nil && q != "" && in.Cursor == "" {
		// The history keeps the Vietnamese name, whatever the chip says.
		entry := repository.HistoryEntry{Query: q, SearchedAt: d.d.Now()}
		for _, c := range view.Chips {
			if c.Kind == domain.AttrCategory {
				entry.Category, entry.CategoryCode = names[c.Code], c.Code
				break
			}
		}
		if len(view.Items) > 0 {
			entry.PhotoURL = view.Items[0].CoverThumbURL
		}
		if err = d.d.History.Remember(ctx, *viewer, entry); err != nil {
			d.d.Log.WarnContext(ctx, "remember the search", "err", err)
		}
	}
	if viewer != nil && q != "" {
		id, found, keyErr := d.d.Watches.ByKey(ctx, *viewer, domain.HistoryKey(q))
		switch {
		case keyErr != nil:
			d.d.Log.WarnContext(ctx, "read the watch of the query", "err", keyErr)
		case found:
			view.WatchID = &id
		default:
		}
	}
	if in.Watch != uuid.Nil() && in.Cursor == "" {
		if err = d.d.Watches.Seen(ctx, *viewer, in.Watch); err != nil {
			d.d.Log.WarnContext(ctx, "see the watch", "err", err)
		}
	}
	return view, nil
}

// understand is the model's reading of account's q, from the cache when the
// same query was read within domain.CacheTTL, and whether the model
// answered. A call counts against the account's domain.ModelCallsPerDay; past
// it, or with the count unreadable, the model is not asked. The call runs
// apart from ctx, so a caller gone still caches the answer, and the searches
// of one query share it. A model that errs or outlasts the timeout gives the
// zero reading. Search and Watch call it.
func (d *Discovery) understand(
	ctx context.Context, account uuid.UUID, q string, categories []catalogdomain.Category, now time.Time,
) (domain.Understanding, bool) {
	if u, found := d.cached(ctx, q, now); found {
		return u, true
	}
	key := domain.QueryKey(q)
	taken, err := d.d.ModelUses.Take(ctx, account, now, domain.ModelCallsPerDay)
	if err != nil {
		d.d.Log.WarnContext(ctx, "count the model call", "err", err)
	}
	if !taken {
		d.d.Log.InfoContext(ctx, "searched without the model", "reason", "the day's model calls are used or uncounted")
		return domain.Understanding{}, false
	}
	read := d.reads.DoChan(key, func() (any, error) {
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.d.ModelTimeout)
		defer cancel()
		var answer domain.Understanding
		err := d.d.LLM.Complete(mctx, llm.Request{
			System: domain.SystemPrompt(categories, now), User: q, SchemaName: "search_understanding", Schema: domain.Schema(),
		}, &answer)
		if err != nil {
			d.d.Log.WarnContext(mctx, "the model did not read the query", "err", err)
			return nil, fmt.Errorf("read the query: %w", err)
		}
		b, err := json.Marshal(answer)
		if err == nil {
			err = d.d.Understandings.Put(mctx, key, domain.PromptVersion, b, now, now.Add(-domain.CacheTTL))
		}
		if err != nil {
			d.d.Log.WarnContext(mctx, "cache the understanding", "err", err)
		}
		return answer, nil
	})
	select {
	case r := <-read:
		if r.Err != nil {
			return domain.Understanding{}, false
		}
		answer, ok := r.Val.(domain.Understanding)
		return answer, ok
	case <-ctx.Done():
		return domain.Understanding{}, false
	}
}

// cached is the model's answer for q stored within domain.CacheTTL, and
// whether there is one. Search and understand call it.
func (d *Discovery) cached(ctx context.Context, q string, now time.Time) (domain.Understanding, bool) {
	var u domain.Understanding
	answer, found, err := d.d.Understandings.Get(ctx, domain.QueryKey(q), domain.PromptVersion, now.Add(-domain.CacheTTL))
	if err != nil {
		d.d.Log.WarnContext(ctx, "read the cached understanding", "err", err)
		return u, false
	}
	return u, found && json.Unmarshal(answer, &u) == nil
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
	now := d.d.Now()
	v, found, err := d.d.QueryVectors.Get(ctx, version, text, now)
	if err != nil {
		d.d.Log.WarnContext(ctx, "read the cached query vector", "err", err)
	}
	if found {
		return v
	}
	vs, err := m.Embed(ctx, []string{text})
	if err != nil {
		d.d.Log.WarnContext(ctx, "embed the query", "version", version, "err", err)
		return nil
	}
	if err = d.d.QueryVectors.Put(ctx, version, text, vs[0], now); err != nil {
		d.d.Log.WarnContext(ctx, "cache the query vector", "err", err)
	}
	d.startVectorSweep(ctx, now)
	return vs[0]
}

// startVectorSweep starts the day's sweep of the text vectors once per
// process and day, after a write; Restate runs one per day.
func (d *Discovery) startVectorSweep(ctx context.Context, now time.Time) {
	day := now.UTC().Format(time.DateOnly)
	if d.d.Sweeps == nil || d.vectorDay.Swap(day) == day {
		return
	}
	if err := d.d.Sweeps.SweepVectors(ctx, day); err != nil {
		d.vectorDay.Store("")
		d.d.Log.WarnContext(ctx, "start the vector sweep", "err", err)
	}
}

// SweepVectors deletes the text vectors that fell out of use and the least
// used past domain.VectorCacheMax.
func (d *Discovery) SweepVectors(ctx context.Context) (int64, error) {
	n, err := d.d.QueryVectors.Sweep(ctx, d.d.Now(), domain.VectorKeepUnused, domain.VectorKeepUsed, domain.VectorCacheMax)
	if err != nil {
		return 0, fmt.Errorf("sweep the text vectors: %w", err)
	}
	d.d.Log.InfoContext(ctx, "swept the text vectors", "deleted", n)
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
	saved, err := d.d.Saves.SavedAmong(ctx, *viewer, ids)
	if err != nil {
		return nil, fmt.Errorf("read saves: %w", err)
	}
	for i := range out {
		out[i].Saved = saved[out[i].ID]
	}
	return out, nil
}
