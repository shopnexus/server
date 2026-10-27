package domain_test

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
	"time"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/discovery/domain"
)

var (
	now    = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	hanoi  = catalogdomain.Point{Lat: 21.02, Lng: 105.84}
	origin = catalogdomain.Point{Lat: 10.77, Lng: 106.70}
	vocab  = domain.Vocabulary{
		Categories: map[string]string{"vehicles": "Xe cộ", "vehicles.bicycle": "Xe đạp", "sports": "Thể thao"},
		Place: func(name string) (catalogdomain.Point, bool) {
			return hanoi, name == "Hà Nội"
		},
	}
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func boosts(c domain.Compiled, kind catalogdomain.BoostKind) []catalogdomain.Boost {
	var out []catalogdomain.Boost
	for _, b := range c.Boosts {
		if b.Kind == kind {
			out = append(out, b)
		}
	}
	return out
}

func chipIDs(c domain.Compiled) []string {
	out := make([]string, 0, len(c.Chips))
	for _, ch := range c.Chips {
		out = append(out, ch.ID)
	}
	return out
}

func TestCompileNothing(t *testing.T) {
	t.Parallel()
	c := domain.Compile(domain.Understanding{}, "xe dap", vocab, nil, now, nil)
	if len(c.Phrases) != 1 || c.Phrases[0].Text != "xe dap" || !near(c.Phrases[0].Weight, domain.RawWeight) ||
		len(c.Boosts) != 0 || len(c.Chips) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestCompileCategory(t *testing.T) {
	t.Parallel()
	u := domain.Understanding{Boosts: []domain.Signal{{Attr: domain.AttrCategory, Values: []string{"vehicles.bicycle", "nope", "vehicles"}}}}
	c := domain.Compile(u, "xe", vocab, nil, now, nil)
	sub, root := boosts(c, catalogdomain.BoostCategory), boosts(c, catalogdomain.BoostCategoryRoot)
	if len(sub) != 1 || sub[0].Code != "vehicles.bicycle" || !near(sub[0].Weight, 1.5) ||
		len(root) != 1 || root[0].Code != "vehicles" || !near(root[0].Weight, 1.5*0.33) {
		t.Fatalf("%+v", c.Boosts)
	}
	if ids := chipIDs(c); !slices.Equal(ids, []string{"category-0", "category-2"}) || c.Chips[0].Label != "Xe đạp" {
		t.Fatalf("chips %+v", c.Chips)
	}
	off := domain.Compile(u, "xe", vocab, nil, now, []string{"category-0"})
	if len(boosts(off, catalogdomain.BoostCategory)) != 0 || slices.Contains(chipIDs(off), "category-0") {
		t.Fatalf("off: %+v", off)
	}
}

func TestCompilePrice(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]struct {
		lo, hi int64
		label  string
	}{
		"-3000000":        {0, 3_000_000, "Dưới 3 triệu"},
		"1000000-2000000": {1_000_000, 2_000_000, "1–2 triệu"},
		"-500000":         {0, 500_000, "Dưới 500 nghìn"},
		"5000000-":        {5_000_000, 0, "Từ 5 triệu"},
		"-1500000":        {0, 1_500_000, "Dưới 1,5 triệu"},
	} {
		u := domain.Understanding{Boosts: []domain.Signal{{Attr: domain.AttrPrice, Values: []string{value}}}}
		c := domain.Compile(u, "x", vocab, nil, now, nil)
		p := boosts(c, catalogdomain.BoostPrice)
		if len(p) != 1 || p[0].PriceMin != want.lo || p[0].PriceMax != want.hi || !near(p[0].Weight, 1.5) ||
			len(c.Chips) != 1 || c.Chips[0].Label != want.label {
			t.Errorf("%s: %+v %+v", value, p, c.Chips)
		}
	}
	for _, bad := range []string{"abc", "-", "3-1", "-x"} {
		u := domain.Understanding{Boosts: []domain.Signal{{Attr: domain.AttrPrice, Values: []string{bad}}}}
		if c := domain.Compile(u, "x", vocab, nil, now, nil); len(c.Boosts) != 0 {
			t.Errorf("%q: %+v", bad, c.Boosts)
		}
	}
}

func TestCompilePlaces(t *testing.T) {
	t.Parallel()
	nearMe := domain.Understanding{Boosts: []domain.Signal{{Attr: domain.AttrNearMe}}}
	c := domain.Compile(nearMe, "x", vocab, &origin, now, nil)
	rings := boosts(c, catalogdomain.BoostNear)
	if len(rings) != 3 || rings[0].Near != origin || rings[0].RadiusKm != 2 || !near(rings[0].Weight, 1.25) ||
		rings[2].RadiusKm != 15 || len(c.Chips) != 1 || c.Chips[0].Label != "Gần bạn" {
		t.Fatalf("near me: %+v %+v", rings, c.Chips)
	}
	if c = domain.Compile(nearMe, "x", vocab, nil, now, nil); len(c.Boosts) != 0 {
		t.Fatalf("near me without an origin: %+v", c.Boosts)
	}
	place := domain.Understanding{Boosts: []domain.Signal{{Attr: domain.AttrPlace, Values: []string{"Hà Nội", "Atlantis"}}}}
	c = domain.Compile(place, "x", vocab, nil, now, nil)
	if rings = boosts(c, catalogdomain.BoostNear); len(rings) != 3 || rings[0].Near != hanoi || len(c.Chips) != 1 {
		t.Fatalf("place: %+v", c)
	}
}

func TestCompileFlags(t *testing.T) {
	t.Parallel()
	u := domain.Understanding{Boosts: []domain.Signal{
		{Attr: domain.AttrFresh}, {Attr: domain.AttrOffers}, {Attr: domain.AttrCondition, Values: []string{"like_new", "broken"}},
	}}
	c := domain.Compile(u, "x", vocab, nil, now, nil)
	fresh := boosts(c, catalogdomain.BoostFresh)
	if len(fresh) != 2 || !fresh[0].Since.Equal(now.Add(-72*time.Hour)) || !near(fresh[0].Weight, 1.0) || !near(fresh[1].Weight, 0.5) {
		t.Fatalf("fresh: %+v", fresh)
	}
	if o := boosts(c, catalogdomain.BoostOffers); len(o) != 1 || !near(o[0].Weight, 0.75) {
		t.Fatalf("offers: %+v", o)
	}
	if cond := boosts(c, catalogdomain.BoostCondition); len(cond) != 1 || cond[0].Condition != catalogdomain.ConditionLikeNew {
		t.Fatalf("condition: %+v", cond)
	}
}

func TestCompileDemote(t *testing.T) {
	t.Parallel()
	u := domain.Understanding{Demotes: []domain.Signal{{Attr: domain.AttrCategory, Values: []string{"sports"}}}}
	c := domain.Compile(u, "x", vocab, nil, now, nil)
	if root := boosts(c, catalogdomain.BoostCategoryRoot); len(root) != 1 || !near(root[0].Weight, -1.5) || len(c.Chips) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestCompilePhrases(t *testing.T) {
	t.Parallel()
	u := domain.Understanding{Boosts: []domain.Signal{{Attr: domain.AttrPhrase, Values: []string{"xe đạp touring", "xe đạp"}}}}
	c := domain.Compile(u, "xe dap touring", vocab, nil, now, nil)
	if len(c.Phrases) != 3 || !near(c.Phrases[0].Weight, 1) || !near(c.Phrases[1].Weight, 0.5) ||
		c.Phrases[2].Text != "xe dap touring" || !near(c.Phrases[2].Weight, domain.RawWeight) {
		t.Fatalf("%+v", c.Phrases)
	}
	same := domain.Compile(u, "  Xe ĐẠP  ", vocab, nil, now, nil)
	if len(same.Phrases) != 2 {
		t.Fatalf("the raw query the model named is not added again: %+v", same.Phrases)
	}
}

func TestCompileDropsWhatItCannotRead(t *testing.T) {
	t.Parallel()
	u := domain.Understanding{Boosts: []domain.Signal{
		{Attr: "colour", Values: []string{"red"}},
		{Attr: domain.AttrPhrase, Values: []string{"", " ", "a", "b", "c", "d"}},
	}}
	c := domain.Compile(u, "q", vocab, nil, now, nil)
	if len(c.Phrases) != 2 || c.Phrases[0].Text != "a" || len(c.Boosts) != 0 {
		t.Fatalf("only the first three values count, empty ones drop: %+v", c)
	}
}

func TestQueryKey(t *testing.T) {
	t.Parallel()
	if k := domain.QueryKey("  Xe  ĐẠP "); k != "xe đạp" {
		t.Fatalf("%q", k)
	}
}

func TestNearbyBoosts(t *testing.T) {
	t.Parallel()
	b := domain.NearbyBoosts(origin, now)
	if len(b) != 5 || b[0].Kind != catalogdomain.BoostNear || b[4].Kind != catalogdomain.BoostFresh {
		t.Fatalf("%+v", b)
	}
}

func TestSchemaIsJSON(t *testing.T) {
	t.Parallel()
	var v map[string]any
	if err := json.Unmarshal(domain.Schema(), &v); err != nil || v["type"] != "object" {
		t.Fatalf("%v %v", v, err)
	}
}
