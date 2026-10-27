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

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/service"
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

// put posts an active listing at a point.
func (e env) put(t *testing.T, title, category string, p catalogdomain.Point) uuid.UUID {
	t.Helper()
	photo := uuid.NewV7()
	now := time.Now()
	l, err := catalogdomain.NewListing(uuid.NewV7(), e.account(t), catalogdomain.Draft{
		Title: title, Price: 1_500_000, CategoryCode: category, Condition: catalogdomain.ConditionUsed,
		CommuneCode: "00004", Photos: []uuid.UUID{photo},
	}, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true, CommunePoint: p}, now)
	if err != nil {
		t.Fatal(err)
	}
	l.Published(map[uuid.UUID]catalogdomain.PhotoKeys{photo: {Photo: "media/a.jpg", Thumb: "media/a_thumb.jpg"}}, now)
	if err = e.listings.Insert(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l.ID()
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
	me := e.account(t)
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

func TestNearby(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	far := e.put(t, "Bàn gỗ", "furniture.table", catalogdomain.Point{Lat: 21.20, Lng: 105.82})
	closeBy := e.put(t, "Ghế gỗ", "furniture.table", baDinh)
	if _, err := e.discovery.Nearby(ctx, nil, service.Near{}, "", 0); !errors.Is(err, domain.ErrPositionRequired) {
		t.Fatalf("no position: %v", err)
	}
	v, err := e.discovery.Nearby(ctx, nil, service.Near{Lat: baDinh.Lat, Lng: baDinh.Lng}, "", 0)
	if err != nil || len(v.Items) != 2 || v.Items[0].ID != closeBy || v.Items[1].ID != far || *v.Items[1].DistanceKm != 18 {
		t.Fatalf("%+v %v", v, err)
	}
	if !strings.Contains(v.Items[0].Commune, "Ba Đình") && v.Items[0].Commune == "" {
		t.Fatalf("commune: %+v", v.Items[0])
	}
}
