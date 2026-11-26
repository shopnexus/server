package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/service"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/embed/embedtest"
	"github.com/shopnexus/server/internal/provider/llm"
)

// fakeModel answers from a map by the query, counts its calls, and errs,
// stalls or answers after delay on request.
type fakeModel struct {
	mu      sync.Mutex
	answers map[string]string
	calls   int
	fail    bool
	stall   bool
	delay   time.Duration
}

func (m *fakeModel) Complete(ctx context.Context, r llm.Request, out any) error {
	m.mu.Lock()
	m.calls++
	answer, fail, stall, delay := m.answers[r.User], m.fail, m.stall, m.delay
	m.mu.Unlock()
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	if stall {
		<-ctx.Done()
		return ctx.Err()
	}
	if fail {
		return errors.New("model down")
	}
	if answer == "" {
		answer = `{"boosts":[],"demotes":[]}`
	}
	return json.Unmarshal([]byte(answer), out)
}

func (m *fakeModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

var baDinh = catalogdomain.Point{Lat: 21.03, Lng: 105.82}

// put puts a listing of a new seller's on sale, titled title in category,
// its commune at p.
func (e env) put(t *testing.T, title, category string, p catalogdomain.Point) uuid.UUID {
	t.Helper()
	return catalogtest.Listing(t, e.db, accounttest.New(t, e.db), func(d *catalogdomain.ListingInput, r *catalogdomain.Refs) {
		d.Title, d.CategoryCode, r.CommunePoint = title, category, p
	})
}

func TestSearchUnderstands(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	lamp := e.put(t, "Đèn xe đạp", "sports.other", baDinh)
	bike := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"boosts":[{"attr":"phrase","values":["xe đạp"]},` +
		`{"attr":"category","values":["vehicles.bicycle"]}],"demotes":[]}`
	v, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap"})
	// The phrase and the category read "xe đạp" alike, so one chip stands for both.
	if err != nil || len(v.Items) != 2 || v.Items[0].ID != bike || v.Items[1].ID != lamp ||
		len(v.Chips) != 1 || v.Chips[0].ID != "phrase-0" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "  Xe  DAP "}); err != nil || e.model.count() != 1 {
		t.Fatalf("the same query again calls the model: %d %v", e.model.count(), err)
	}
	v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap", Off: []string{"phrase-0"}})
	if err != nil || e.model.count() != 1 || len(v.Chips) != 0 {
		t.Fatalf("off: %d %+v %v", e.model.count(), v.Chips, err)
	}
	e.model.answers["xe dap ha noi"] = `{"boosts":[{"attr":"place","values":[" Hà Nội ","Atlantis"]}],"demotes":[]}`
	v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap ha noi"})
	if err != nil || len(v.Chips) != 1 || v.Chips[0].ID != "place-0" || v.Chips[0].Label != "Hà Nội" {
		t.Fatalf("place: %+v %v", v.Chips, err)
	}
}

// A guest's query skips the model and the embedding: the raw words search
// through BM25, with no chips, and the cursor keeps the next pages on them.
func TestSearchGuestSkipsTheModel(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.put(t, "Xe đạp Giant", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"boosts":[{"attr":"category","values":["vehicles.bicycle"]}],"demotes":[]}`
	e.text.Set("xe dap", embedtest.Axis(catalogdomain.TextDims, 0))
	v, err := e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap", Limit: 1})
	if err != nil || len(v.Items) != 1 || len(v.Chips) != 0 || v.NextCursor != "1r" || e.model.count() != 0 || e.text.Calls() != 0 {
		t.Fatalf("guest: %+v %d %d %v", v, e.model.count(), e.text.Calls(), err)
	}
	if v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap", Limit: 1}); err != nil || len(v.Chips) != 1 || e.model.count() != 1 {
		t.Fatalf("signed in: %+v %d %v", v, e.model.count(), err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap", Limit: 1}); err != nil || len(v.Chips) != 0 ||
		len(v.PreviewChips) != 1 || v.PreviewChips[0].Label != "Xe đạp" || e.model.count() != 1 {
		t.Fatalf("a guest gets the cached chips unapplied: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap", Limit: 1, Cursor: v.NextCursor}); err != nil || len(v.PreviewChips) != 0 {
		t.Fatalf("only the first page previews: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap moi"}); err != nil || len(v.PreviewChips) != 0 {
		t.Fatalf("a query nobody signed in searched: %+v %v", v, err)
	}
	// A guest's words match the titles with diacritics folded away; a query
	// the model read matches them as written.
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "dap"}); err != nil || len(v.Items) != 2 {
		t.Fatalf("guest, folded: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "dap"}); err != nil || len(v.Items) != 0 {
		t.Fatalf("signed in, as written: %+v %v", v, err)
	}
}

// Past the day's model calls a signed-in search runs on the raw words; a
// cached answer still serves.
func TestSearchStopsAtTheDaysModelCalls(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"boosts":[{"attr":"category","values":["vehicles.bicycle"]}],"demotes":[]}`
	if _, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `UPDATE search_model_uses SET calls = $1 WHERE account_id = $2`, domain.ModelCallsPerDay, e.me); err != nil {
		t.Fatal(err)
	}
	v, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap cu"})
	if err != nil || len(v.Chips) != 0 || e.model.count() != 1 {
		t.Fatalf("past the cap: %+v %d %v", v, e.model.count(), err)
	}
	if v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap"}); err != nil || len(v.Chips) != 1 {
		t.Fatalf("a cached answer past the cap: %+v %v", v, err)
	}
}

func TestSearchWithoutTheModel(t *testing.T) {
	t.Parallel()
	for name, set := range map[string]func(*fakeModel){
		"down":  func(m *fakeModel) { m.fail = true },
		"stall": func(m *fakeModel) { m.stall = true },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			set(e.model)
			e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
			v, err := e.discovery.Search(context.Background(), &e.me, service.SearchInput{Query: "xe đạp"})
			if err != nil || len(v.Items) != 1 || len(v.Chips) != 0 {
				t.Fatalf("%+v %v", v, err)
			}
		})
	}
}

// Searches of one query at once share one model call and all get its
// answer.
func TestSearchSharesTheModelCall(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"boosts":[{"attr":"category","values":["vehicles.bicycle"]}],"demotes":[]}`
	e.model.delay = 100 * time.Millisecond
	var wg sync.WaitGroup
	views := make([]service.SearchView, 3)
	for i := range views {
		wg.Go(func() {
			v, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap"})
			if err != nil {
				t.Error(err)
			}
			views[i] = v
		})
	}
	wg.Wait()
	for _, v := range views {
		if len(v.Chips) != 1 {
			t.Fatalf("a search without the answer: %+v", v)
		}
	}
	if e.model.count() != 1 {
		t.Fatalf("three searches of one query called the model %d times", e.model.count())
	}
}

// The pages of a search the model did not answer stay on the raw words
// after the model comes back.
func TestSearchPinsTheRawWords(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.put(t, "Xe đạp Giant", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"boosts":[{"attr":"category","values":["vehicles.bicycle"]}],"demotes":[]}`
	e.model.fail = true
	v, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap", Limit: 1})
	if err != nil || len(v.Chips) != 0 || v.NextCursor != "1r" {
		t.Fatalf("page 1 without the model: %+v %v", v, err)
	}
	e.model.mu.Lock()
	e.model.fail = false
	e.model.mu.Unlock()
	calls := e.model.count()
	page, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap", Limit: 1, Cursor: v.NextCursor})
	if err != nil || len(page.Items) != 1 || len(page.Chips) != 0 || e.model.count() != calls {
		t.Fatalf("page 2 asked the model: %+v %d %v", page, e.model.count(), err)
	}
	if v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap", Limit: 1}); err != nil || len(v.Chips) != 1 || v.NextCursor != "1" {
		t.Fatalf("a new search: %+v %v", v, err)
	}
}

func TestSearchRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	for name, c := range map[string]struct {
		in   service.SearchInput
		want error
	}{
		"nearest without a position":  {service.SearchInput{Sort: catalogdomain.SortNearest}, domain.ErrPositionRequired},
		"a radius without a position": {service.SearchInput{RadiusKm: 5}, domain.ErrPositionRequired},
		"a latitude out of range":     {service.SearchInput{Near: catalogdomain.Near{Lat: 95, Lng: 1}}, catalogdomain.ErrInvalidPosition},
		"a latitude alone":            {service.SearchInput{Near: catalogdomain.Near{Lat: 10}}, catalogdomain.ErrInvalidPosition},
		"an unknown commune":          {service.SearchInput{Near: catalogdomain.Near{Commune: "nope"}}, catalogdomain.ErrInvalidPosition},
		"a word for a cursor":         {service.SearchInput{Cursor: "x"}, domain.ErrInvalidCursor},
		"a cursor past the end":       {service.SearchInput{Cursor: "600"}, domain.ErrInvalidCursor},
		"a mark alone":                {service.SearchInput{Cursor: "r"}, domain.ErrInvalidCursor},
		"a minimum above the maximum": {service.SearchInput{PriceMin: 5_000_000, PriceMax: 1_000_000}, domain.ErrInvalidPriceRange},
		"an unknown province":         {service.SearchInput{Province: "99"}, catalogdomain.ErrInvalidProvince},
	} {
		if _, err := e.discovery.Search(ctx, nil, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if v, err := e.discovery.Search(ctx, nil, service.SearchInput{Query: "?"}); err != nil || len(v.Items) != 0 {
		t.Fatalf("punctuation: %+v %v", v, err)
	}
}

func TestSearchPagesAndSaves(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	bikes := make([]uuid.UUID, 0, 3)
	for range 3 {
		bikes = append(bikes, e.put(t, "Xe đạp", "vehicles.bicycle", baDinh))
	}
	v, err := e.discovery.Search(ctx, nil, service.SearchInput{Category: "vehicles", Limit: 2})
	if err != nil || len(v.Items) != 2 || v.NextCursor != "2" {
		t.Fatalf("first page: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Category: "vehicles", Limit: 2, Cursor: "2"}); err != nil ||
		len(v.Items) != 1 || v.NextCursor != "" {
		t.Fatalf("second page: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe đạp", Category: "vehicles"}); err != nil || len(v.Items) != 3 {
		t.Fatalf("words: %+v %v", v, err)
	}
	me := accounttest.New(t, e.db)
	if err = e.discovery.Save(ctx, me, bikes[0]); err != nil {
		t.Fatal(err)
	}
	v, err = e.discovery.Search(ctx, &me, service.SearchInput{Category: "vehicles.bicycle"})
	saved := 0
	for _, it := range v.Items {
		if it.Saved {
			saved++
			if it.ID != bikes[0] {
				t.Fatalf("saved the wrong one: %+v", it)
			}
		}
	}
	if err != nil || saved != 1 {
		t.Fatalf("saved flags: %+v %v", v, err)
	}
}

// A province keeps its listings, with the other filters.
func TestSearchProvince(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	hanoi := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.put(t, "iPhone 12", "phones.iphone", baDinh)
	saigon := catalogtest.Listing(t, e.db, accounttest.New(t, e.db), func(d *catalogdomain.ListingInput, r *catalogdomain.Refs) {
		d.CommuneCode, r.CommunePoint = "25747", catalogdomain.Point{Lat: 10.97, Lng: 106.66}
	})
	v, err := e.discovery.Search(ctx, nil, service.SearchInput{Province: "79"})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != saigon {
		t.Fatalf("browse: %+v %v", v, err)
	}
	v, err = e.discovery.Search(ctx, nil, service.SearchInput{Province: "01", Category: "vehicles", Sort: catalogdomain.SortPriceAsc})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != hanoi {
		t.Fatalf("a root in Hà Nội: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Province: "79", Category: "phones"}); err != nil || len(v.Items) != 0 {
		t.Fatalf("no phones in TP Hồ Chí Minh: %+v %v", v, err)
	}
}

// A seller keeps that seller's listings; the search keeps no history,
// names no watch and goes without watch.
func TestSearchSeller(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller := accounttest.New(t, e.db)
	bike := catalogtest.Listing(t, e.db, seller, func(d *catalogdomain.ListingInput, _ *catalogdomain.Refs) {
		d.Title, d.CategoryCode = "Xe đạp Asama", "vehicles.bicycle"
	})
	e.put(t, "Xe đạp Giant", "vehicles.bicycle", baDinh)
	w, _, err := e.discovery.Watch(ctx, e.me, service.WatchInput{Query: "xe đạp"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe đạp", Seller: seller})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != bike || v.WatchID != nil {
		t.Fatalf("q: %+v %v", v, err)
	}
	if h, _ := e.discovery.History(ctx, e.me); len(h.Items) != 0 {
		t.Fatalf("history: %+v", h)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Seller: seller, Category: "vehicles"}); err != nil || len(v.Items) != 1 {
		t.Fatalf("browse: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Seller: uuid.NewV7()}); err != nil || len(v.Items) != 0 {
		t.Fatalf("an unknown seller: %+v %v", v, err)
	}
	if err = e.accounts.Block(ctx, seller, e.me); err != nil {
		t.Fatal(err)
	}
	if v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Seller: seller}); err != nil || len(v.Items) != 0 {
		t.Fatalf("a seller who blocked me: %+v %v", v, err)
	}
	if _, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Watch: w.ID, Seller: seller}); !errors.Is(err, domain.ErrSearchConflict) {
		t.Fatalf("watch with seller: %v", err)
	}
}

func TestSearchNearby(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	far := e.put(t, "Bàn gỗ", "furniture.table", catalogdomain.Point{Lat: 21.20, Lng: 105.82})
	closeBy := e.put(t, "Ghế gỗ", "furniture.table", baDinh)
	e.put(t, "Bàn xa", "furniture.table", catalogdomain.Point{Lat: 10.78, Lng: 106.70})
	nearby := func(in service.SearchInput) (service.SearchView, error) {
		in.Sort = service.SortNearby
		return e.discovery.Search(ctx, nil, in)
	}
	if _, err := nearby(service.SearchInput{}); !errors.Is(err, domain.ErrPositionRequired) {
		t.Fatalf("no position: %v", err)
	}
	if _, err := nearby(service.SearchInput{Query: "ghế", Near: catalogdomain.Near{Commune: "00004"}}); !errors.Is(err, domain.ErrSearchConflict) {
		t.Fatalf("with q: %v", err)
	}
	if _, err := nearby(service.SearchInput{Like: closeBy, Near: catalogdomain.Near{Commune: "00004"}}); !errors.Is(err, domain.ErrSearchConflict) {
		t.Fatalf("with like: %v", err)
	}
	v, err := nearby(service.SearchInput{Near: catalogdomain.Near{Lat: baDinh.Lat, Lng: baDinh.Lng}})
	if err != nil || len(v.Items) != 2 || v.Items[0].ID != closeBy || v.Items[1].ID != far || *v.Items[1].DistanceKm != 18 {
		t.Fatalf("%+v %v", v, err)
	}
	if !strings.Contains(v.Items[0].Commune, "Ba Đình") && v.Items[0].Commune == "" {
		t.Fatalf("commune: %+v", v.Items[0])
	}
	if v, err = nearby(service.SearchInput{Near: catalogdomain.Near{Commune: "00004"}}); err != nil || len(v.Items) != 2 || v.Items[0].ID != closeBy {
		t.Fatalf("from a commune: %+v %v", v, err)
	}
	if v, err = nearby(service.SearchInput{Near: catalogdomain.Near{Commune: "00004"}, Limit: 1}); err != nil || len(v.Items) != 1 || v.Items[0].ID != closeBy || v.NextCursor == "" {
		t.Fatalf("page 1: %+v %v", v, err)
	}
	if v, err = nearby(service.SearchInput{Near: catalogdomain.Near{Commune: "00004"}, Cursor: v.NextCursor, Limit: 1}); err != nil || len(v.Items) != 1 || v.Items[0].ID != far {
		t.Fatalf("page 2: %+v %v", v, err)
	}
}

// vec gives listing a text vector and a photo vector, as ListingEmbed would.
func (e env) vec(t *testing.T, id uuid.UUID, text, photo []float32) {
	t.Helper()
	vs := catalogrepo.NewVectors(e.db)
	if text != nil {
		if err := vs.PutText(context.Background(), id, "text-test", []byte{1}, text, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if photo != nil {
		if err := vs.PutPhoto(context.Background(), id, "media/a.jpg", "image-test", photo, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchFindsByMeaning(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	kid := e.put(t, "Xe cho bé 4 tuổi", "vehicles.bicycle", baDinh)
	pot := e.put(t, "Nồi cơm điện", "home.kitchen", baDinh)
	e.vec(t, kid, embedtest.Axis(catalogdomain.TextDims, 0), nil)
	e.vec(t, pot, embedtest.Axis(catalogdomain.TextDims, 1), nil)
	e.model.answers["xe dap tre em"] = `{"rewritten":"xe đạp trẻ em","visual":"",` +
		`"boosts":[{"attr":"phrase","values":["xe đạp trẻ em"]}],"demotes":[]}`
	e.text.Set("xe đạp trẻ em", embedtest.Axis(catalogdomain.TextDims, 0))
	v, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap tre em"})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != kid {
		t.Fatalf("no shared word, found by its text vector: %+v %v", v, err)
	}
	if e.image.Calls() != 0 {
		t.Fatal("an empty visual embeds nothing")
	}
	if _, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap tre em", Cursor: "1"}); err != nil || e.text.Calls() != 1 {
		t.Fatalf("page 2 embeds again: %d %v", e.text.Calls(), err)
	}
	if _, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "noi com"}); err != nil {
		t.Fatal(err)
	}
	e.sweeps.mu.Lock()
	days := e.sweeps.days
	e.sweeps.mu.Unlock()
	if len(days) != 1 || days[0] != time.Now().UTC().Format(time.DateOnly) {
		t.Fatalf("two writes of a day start one vector sweep: %v", days)
	}
}

func TestSearchWithoutEmbeddingsFallsBackToWords(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	bike := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.text.SetFail(errors.New("down"))
	e.image.SetFail(errors.New("down"))
	e.model.answers["xe dap"] = `{"rewritten":"xe đạp","visual":"a bicycle",` +
		`"boosts":[{"attr":"phrase","values":["xe đạp"]}],"demotes":[]}`
	v, err := e.discovery.Search(context.Background(), &e.me, service.SearchInput{Query: "xe dap"})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != bike {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestSearchModelDownEmbedsTheRawQuery(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.model.fail = true
	if _, err := e.discovery.Search(context.Background(), &e.me, service.SearchInput{Query: "ip13 pm"}); err != nil {
		t.Fatal(err)
	}
	if in := e.text.Inputs(); len(in) != 1 || in[0] != "ip13 pm" || e.image.Calls() != 0 {
		t.Fatalf("text %v, image %d", in, e.image.Calls())
	}
}

// A hung embedding model costs a search the model timeout, not the HTTP
// client's: both query embeds run at once under it, and BM25 answers.
func TestSearchHungEmbeddingsFallBackQuickly(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.put(t, "Ghế gỗ sồi", "furniture.other", baDinh)
	e.model.answers["ghế gỗ"] = `{"rewritten":"ghế gỗ","visual":"a wooden chair","boosts":[],"demotes":[]}`
	e.text.SetSlow(time.Minute)
	e.image.SetSlow(time.Minute)
	start := time.Now()
	v, err := e.discovery.Search(context.Background(), &e.me, service.SearchInput{Query: "ghế gỗ"})
	if err != nil || len(v.Items) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("a hung model held the search %v", took)
	}
}

func TestSearchByPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	chair := e.put(t, "Ghế gỗ", "furniture.other", baDinh)
	e.vec(t, chair, nil, embedtest.Axis(catalogdomain.PhotoDims, 0))
	photo := mediatest.Upload(t, e.media, me, "image/jpeg", mediatest.JPEG(t, 1200, 900))
	e.image.SetFail(nil)
	// The fake hashes the data URL; make that input land on axis 0.
	img, err := e.media.QueryImage(ctx, me, photo)
	if err != nil {
		t.Fatal(err)
	}
	e.image.Set(embed.ImageInput(img), embedtest.Axis(catalogdomain.PhotoDims, 0))

	v, err := e.discovery.Search(ctx, &me, service.SearchInput{Photo: photo})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != chair || len(v.Chips) != 0 {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err = e.discovery.Search(ctx, &me, service.SearchInput{Photo: photo, Cursor: "1"}); err != nil || e.image.Calls() != 1 {
		t.Fatalf("page 2 embeds the photo again: %d %v", e.image.Calls(), err)
	}
}

func TestSearchByPhotoRefuses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := accounttest.New(t, e.db), accounttest.New(t, e.db)
	photo := mediatest.Upload(t, e.media, me, "image/jpeg", mediatest.JPEG(t, 64, 48))
	for name, tc := range map[string]struct {
		viewer *uuid.UUID
		in     service.SearchInput
		want   error
	}{
		"with q":         {&me, service.SearchInput{Photo: photo, Query: "ghế"}, domain.ErrSearchConflict},
		"with like":      {&me, service.SearchInput{Photo: photo, Like: uuid.NewV7()}, domain.ErrSearchConflict},
		"signed out":     {nil, service.SearchInput{Photo: photo}, authn.ErrUnauthenticated},
		"someone else's": {&other, service.SearchInput{Photo: photo}, mediadomain.ErrInvalidUpload},
	} {
		if _, err := e.discovery.Search(ctx, tc.viewer, tc.in); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if e.image.Calls() != 0 {
		t.Fatalf("a refused photo was embedded: %d", e.image.Calls())
	}
	e.image.SetFail(errors.New("down"))
	if _, err := e.discovery.Search(ctx, &me, service.SearchInput{Photo: photo}); !errors.Is(err, domain.ErrSearchUnavailable) {
		t.Fatal(err)
	}
}

func TestSearchLike(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me := accounttest.New(t, e.db)
	source := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	twin := e.put(t, "Xe đạp Asama 26 inch", "vehicles.bicycle", baDinh)
	mine := catalogtest.Listing(t, e.db, me) // also "Xe đạp Asama"
	v, err := e.discovery.Search(ctx, &me, service.SearchInput{Like: source})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != twin {
		t.Fatalf("no vectors yet, BM25 and boosts answer; the source and mine stay out: %+v %v (mine %v)", v, err, mine)
	}
	e.vec(t, source, embedtest.Axis(catalogdomain.TextDims, 0), embedtest.Axis(catalogdomain.PhotoDims, 0))
	look := e.put(t, "Xe trẻ em", "vehicles.bicycle", baDinh)
	e.vec(t, look, nil, embedtest.Axis(catalogdomain.PhotoDims, 0))
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Like: source}); err != nil || len(v.Items) != 3 {
		t.Fatalf("signed out: the twin, mine and the look-alike: %+v %v", v, err)
	}
	if e.text.Calls() != 0 || e.image.Calls() != 0 {
		t.Fatal("like embeds nothing at search time")
	}
	if _, err = e.discovery.Search(ctx, nil, service.SearchInput{Like: uuid.NewV7()}); !errors.Is(err, catalogdomain.ErrNotFound) {
		t.Fatal(err)
	}
}

// With the image model off, a photo search is unavailable and q runs without
// the visual list; with the text model off, q runs without the text list.
func TestSearchWithAnEmbedderOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newEnv(t, func(d *service.Deps) { d.Image = nil })
	me := accounttest.New(t, e.db)
	photo := mediatest.Upload(t, e.media, me, "image/jpeg", mediatest.JPEG(t, 64, 48))
	if _, err := e.discovery.Search(ctx, &me, service.SearchInput{Photo: photo}); !errors.Is(err, domain.ErrSearchUnavailable) {
		t.Fatalf("photo search with images off: %v", err)
	}
	bike := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"rewritten":"xe đạp","visual":"a bicycle","boosts":[],"demotes":[]}`
	v, err := e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap"})
	if err != nil || len(v.Items) == 0 || v.Items[0].ID != bike {
		t.Fatalf("q with images off: %+v, %v", v, err)
	}
	if e.image.Calls() != 0 {
		t.Fatalf("the image model was called: %d", e.image.Calls())
	}

	e = newEnv(t, func(d *service.Deps) { d.Text = nil })
	bike = e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"rewritten":"xe đạp","boosts":[],"demotes":[]}`
	if v, err = e.discovery.Search(ctx, &e.me, service.SearchInput{Query: "xe dap"}); err != nil || len(v.Items) == 0 || v.Items[0].ID != bike {
		t.Fatalf("q with text off: %+v, %v", v, err)
	}
	if e.text.Calls() != 0 {
		t.Fatalf("the text model was called: %d", e.text.Calls())
	}
}
