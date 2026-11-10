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

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/service"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// fakeModel answers from a map by the query, counts its calls, and errs or
// stalls on request.
type fakeModel struct {
	mu      sync.Mutex
	answers map[string]string
	calls   int
	fail    bool
	stall   bool
}

func (m *fakeModel) Complete(ctx context.Context, r llm.Request, out any) error {
	m.mu.Lock()
	m.calls++
	answer, fail, stall := m.answers[r.User], m.fail, m.stall
	m.mu.Unlock()
	if stall {
		<-ctx.Done()
		return ctx.Err()
	}
	if fail {
		return errors.New("model down")
	}
	if answer == "" {
		answer = `{"understood":"","boosts":[],"demotes":[]}`
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
	return catalogtest.Listing(t, e.db, accounttest.New(t, e.db), func(d *catalogdomain.Draft, r *catalogdomain.Refs) {
		d.Title, d.CategoryCode, r.CommunePoint = title, category, p
	})
}

func TestSearchUnderstands(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	lamp := e.put(t, "Đèn xe đạp", "sports.other", baDinh)
	bike := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.model.answers["xe dap"] = `{"understood":"Xe đạp","boosts":[{"attr":"phrase","values":["xe đạp"]},` +
		`{"attr":"category","values":["vehicles.bicycle"]}],"demotes":[]}`
	v, err := e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap"})
	if err != nil || len(v.Items) != 2 || v.Items[0].ID != bike || v.Items[1].ID != lamp || v.Understood != "Xe đạp" ||
		len(v.Chips) != 2 || v.Chips[1].ID != "category-0" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "  Xe  DAP "}); err != nil || e.model.count() != 1 {
		t.Fatalf("the same query again calls the model: %d %v", e.model.count(), err)
	}
	v, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap", Off: []string{"category-0"}})
	if err != nil || e.model.count() != 1 || len(v.Chips) != 1 {
		t.Fatalf("off: %d %+v %v", e.model.count(), v.Chips, err)
	}
	e.model.answers["xe dap ha noi"] = `{"understood":"Xe đạp ở Hà Nội","boosts":[{"attr":"place","values":[" Hà Nội ","Atlantis"]}],"demotes":[]}`
	v, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap ha noi"})
	if err != nil || len(v.Chips) != 1 || v.Chips[0].ID != "place-0" || v.Chips[0].Label != "Hà Nội" {
		t.Fatalf("place: %+v %v", v.Chips, err)
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
			v, err := e.discovery.Search(context.Background(), nil, service.SearchInput{Query: "xe đạp"})
			if err != nil || len(v.Items) != 1 || len(v.Chips) != 0 || v.Understood != "" {
				t.Fatalf("%+v %v", v, err)
			}
		})
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
		"a latitude out of range":     {service.SearchInput{Near: service.Near{Lat: 95, Lng: 1}}, domain.ErrInvalidPosition},
		"a latitude alone":            {service.SearchInput{Near: service.Near{Lat: 10}}, domain.ErrInvalidPosition},
		"an unknown commune":          {service.SearchInput{Near: service.Near{Commune: "nope"}}, domain.ErrInvalidPosition},
		"a word for a cursor":         {service.SearchInput{Cursor: "x"}, domain.ErrInvalidCursor},
		"a cursor past the end":       {service.SearchInput{Cursor: "600"}, domain.ErrInvalidCursor},
		"a minimum above the maximum": {service.SearchInput{PriceMin: 5_000_000, PriceMax: 1_000_000}, domain.ErrInvalidPriceRange},
	} {
		if _, err := e.discovery.Search(ctx, nil, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if v, err := e.discovery.Search(ctx, nil, service.SearchInput{Query: "?"}); err != nil || len(v.Items) != 0 {
		t.Fatalf("punctuation: %+v %v", v, err)
	}
}

func TestSearchPagesCountsAndSaves(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	bikes := make([]uuid.UUID, 0, 3)
	for range 3 {
		bikes = append(bikes, e.put(t, "Xe đạp", "vehicles.bicycle", baDinh))
	}
	v, err := e.discovery.Search(ctx, nil, service.SearchInput{Category: "vehicles", Limit: 2})
	if err != nil || len(v.Items) != 2 || v.NextCursor != "2" || len(v.Counts) != 1 || v.Counts[0].Count != 3 {
		t.Fatalf("first page: %+v %v", v, err)
	}
	if v, err = e.discovery.Search(ctx, nil, service.SearchInput{Category: "vehicles", Limit: 2, Cursor: "2"}); err != nil ||
		len(v.Items) != 1 || v.NextCursor != "" {
		t.Fatalf("second page: %+v %v", v, err)
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
	if _, err := nearby(service.SearchInput{Query: "ghế", Near: service.Near{Commune: "00004"}}); !errors.Is(err, domain.ErrSearchConflict) {
		t.Fatalf("with q: %v", err)
	}
	if _, err := nearby(service.SearchInput{Like: closeBy, Near: service.Near{Commune: "00004"}}); !errors.Is(err, domain.ErrSearchConflict) {
		t.Fatalf("with like: %v", err)
	}
	v, err := nearby(service.SearchInput{Near: service.Near{Lat: baDinh.Lat, Lng: baDinh.Lng}})
	if err != nil || len(v.Items) != 2 || v.Items[0].ID != closeBy || v.Items[1].ID != far || *v.Items[1].DistanceKm != 18 {
		t.Fatalf("%+v %v", v, err)
	}
	if !strings.Contains(v.Items[0].Commune, "Ba Đình") && v.Items[0].Commune == "" {
		t.Fatalf("commune: %+v", v.Items[0])
	}
	if v, err = nearby(service.SearchInput{Near: service.Near{Commune: "00004"}}); err != nil || len(v.Items) != 2 || v.Items[0].ID != closeBy {
		t.Fatalf("from a commune: %+v %v", v, err)
	}
	if v, err = nearby(service.SearchInput{Near: service.Near{Commune: "00004"}, Limit: 1}); err != nil || len(v.Items) != 1 || v.Items[0].ID != closeBy || v.NextCursor == "" {
		t.Fatalf("page 1: %+v %v", v, err)
	}
	if v, err = nearby(service.SearchInput{Near: service.Near{Commune: "00004"}, Cursor: v.NextCursor, Limit: 1}); err != nil || len(v.Items) != 1 || v.Items[0].ID != far {
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
	e.model.answers["xe dap tre em"] = `{"understood":"Xe đạp trẻ em","rewritten":"xe đạp trẻ em","visual":"",` +
		`"boosts":[{"attr":"phrase","values":["xe đạp trẻ em"]}],"demotes":[]}`
	e.text.Set("xe đạp trẻ em", embedtest.Axis(catalogdomain.TextDims, 0))
	v, err := e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap tre em"})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != kid {
		t.Fatalf("no shared word, found by its text vector: %+v %v", v, err)
	}
	if e.image.Calls() != 0 {
		t.Fatal("an empty visual embeds nothing")
	}
	if _, err = e.discovery.Search(ctx, nil, service.SearchInput{Query: "xe dap tre em", Cursor: "1"}); err != nil || e.text.Calls() != 1 {
		t.Fatalf("page 2 embeds again: %d %v", e.text.Calls(), err)
	}
}

func TestSearchWithoutEmbeddingsFallsBackToWords(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	bike := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	e.text.SetFail(errors.New("down"))
	e.image.SetFail(errors.New("down"))
	e.model.answers["xe dap"] = `{"understood":"Xe đạp","rewritten":"xe đạp","visual":"a bicycle",` +
		`"boosts":[{"attr":"phrase","values":["xe đạp"]}],"demotes":[]}`
	v, err := e.discovery.Search(context.Background(), nil, service.SearchInput{Query: "xe dap"})
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != bike {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestSearchModelDownEmbedsTheRawQuery(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.model.fail = true
	if _, err := e.discovery.Search(context.Background(), nil, service.SearchInput{Query: "ip13 pm"}); err != nil {
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
	e.model.answers["ghế gỗ"] = `{"understood":"Ghế gỗ","rewritten":"ghế gỗ","visual":"a wooden chair","boosts":[],"demotes":[]}`
	e.text.SetSlow(time.Minute)
	e.image.SetSlow(time.Minute)
	start := time.Now()
	v, err := e.discovery.Search(context.Background(), nil, service.SearchInput{Query: "ghế gỗ"})
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
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != chair || v.Understood != "" || len(v.Chips) != 0 {
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
