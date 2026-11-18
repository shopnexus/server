package service_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/service"
)

func cardIDs(items []service.SearchItem) []uuid.UUID {
	out := make([]uuid.UUID, len(items))
	for i := range items {
		out[i] = items[i].ID
	}
	return out
}

// Dành cho bạn puts the subcategories of the viewer's saves (3), searches
// (2) and views (1) of the last 30 days first, then the newest listings;
// their own, hidden and blocked listings stay out. The rail is the
// subcategory opened that day, without the listings opened. A guest and an
// account without signals get the newest listings.
func TestForYou(t *testing.T) {
	t.Parallel()
	clock := time.Now()
	e := newEnv(t, func(d *service.Deps) { d.Now = func() time.Time { return clock } })
	ctx := context.Background()
	me, fresh, solo := accounttest.New(t, e.db), accounttest.New(t, e.db), accounttest.New(t, e.db)
	fan := e.put(t, "Quạt cây", "home.fan", baDinh)
	bike := e.put(t, "Xe đạp Asama", "vehicles.bicycle", baDinh)
	toyOld := e.put(t, "Ngựa gỗ", "baby.toys", baDinh)
	gymOld := e.put(t, "Tạ tay", "sports.gym", baDinh)
	washer := e.put(t, "Máy giặt Toshiba", "home.washer", baDinh)
	toyNew := e.put(t, "Xếp hình", "baby.toys", baDinh)
	gymNew := e.put(t, "Thảm tập", "sports.gym", baDinh)
	own := catalogtest.Listing(t, e.db, me, func(d *catalogdomain.ListingInput, _ *catalogdomain.Refs) {
		d.Title, d.CategoryCode = "Búp bê", "baby.toys"
	})
	hidden := e.put(t, "Xe đẩy", "baby.toys", baDinh)
	blocked := e.put(t, "Gấu bông", "baby.toys", baDinh)
	blocker := e.put(t, "Bóng nhựa", "baby.toys", baDinh)
	seller := func(id uuid.UUID) uuid.UUID {
		t.Helper()
		s, _, err := e.catalog.Summary(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return s.SellerID
	}
	if err := e.discovery.Hide(ctx, me, hidden); err != nil {
		t.Fatal(err)
	}
	if err := e.accounts.Block(ctx, me, seller(blocked)); err != nil {
		t.Fatal(err)
	}
	if err := e.accounts.Block(ctx, seller(blocker), me); err != nil {
		t.Fatal(err)
	}
	e.model.answers["quat"] = `{"understood":"Quạt","boosts":[{"attr":"category","values":["home.fan"]}],"demotes":[]}`
	e.model.answers["may giat"] = `{"understood":"Máy giặt","boosts":[{"attr":"category","values":["home.washer"]}],"demotes":[]}`

	// Forty days ago is outside the window: these weigh nothing.
	clock = time.Now().AddDate(0, 0, -40)
	if err := e.discovery.Save(ctx, me, fan); err != nil {
		t.Fatal(err)
	}
	if err := e.discovery.Viewed(ctx, me, fan); err != nil {
		t.Fatal(err)
	}
	if _, err := e.discovery.Search(ctx, &me, service.SearchInput{Query: "quat"}); err != nil {
		t.Fatal(err)
	}
	clock = time.Now()
	if err := e.discovery.Save(ctx, me, toyOld); err != nil {
		t.Fatal(err)
	}
	if _, err := e.discovery.Search(ctx, &me, service.SearchInput{Query: "may giat"}); err != nil {
		t.Fatal(err)
	}
	if err := e.discovery.Viewed(ctx, me, gymOld); err != nil {
		t.Fatal(err)
	}
	if err := e.discovery.Viewed(ctx, solo, washer); err != nil {
		t.Fatal(err)
	}

	got, err := e.discovery.ForYou(ctx, &me, catalogdomain.Near{Commune: "00004"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []uuid.UUID{toyNew, toyOld, washer, gymNew, gymOld, bike, fan}; !slices.Equal(cardIDs(got.Items), want) {
		t.Fatalf("feed: %v, want %v", cardIDs(got.Items), want)
	}
	if !got.Items[1].Saved || got.Items[0].Saved || got.Items[0].DistanceKm == nil {
		t.Fatalf("saved and distance: %+v", got.Items[:2])
	}
	if got.Viewed == nil || got.Viewed.Category != "sports.gym" || !slices.Equal(cardIDs(got.Viewed.Items), []uuid.UUID{gymNew}) {
		t.Fatalf("rail: %+v", got.Viewed)
	}

	newest := []uuid.UUID{blocker, blocked, hidden, own, gymNew, toyNew, washer, gymOld, toyOld, bike, fan}
	for name, viewer := range map[string]*uuid.UUID{"a guest": nil, "no signals": &fresh} {
		got, err = e.discovery.ForYou(ctx, viewer, catalogdomain.Near{})
		if err != nil || !slices.Equal(cardIDs(got.Items), newest) || got.Viewed != nil || got.Items[0].DistanceKm != nil {
			t.Errorf("%s: %v %+v %v", name, cardIDs(got.Items), got.Viewed, err)
		}
	}
	// solo opened the only washer: the rail has nothing else to show.
	if got, err = e.discovery.ForYou(ctx, &solo, catalogdomain.Near{}); err != nil || got.Viewed != nil || got.Items[0].ID != washer {
		t.Fatalf("solo: %v %+v %v", cardIDs(got.Items), got.Viewed, err)
	}
}
