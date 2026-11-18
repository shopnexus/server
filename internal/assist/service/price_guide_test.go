package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/platform/apperr"
)

// bike is a guide's draft: a used bicycle in commune 00004.
func bike() service.PriceGuideInput {
	return service.PriceGuideInput{
		Title: "Xe đạp Asama", CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
		Specs: []catalogdomain.Spec{{Label: "Khung", Value: "nhôm"}}, Description: "Đi ít", CommuneCode: "00004",
	}
}

// UC-LST-01 A3.
func TestPriceGuide(t *testing.T) {
	t.Parallel()
	var cat *sampleOf
	e := newEnv(t, func(x *env) {
		cat = &sampleOf{Service: x.deps.Catalog}
		x.deps.Catalog = cat
	})
	ctx := context.Background()
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	far := catalogdomain.Point{Lat: 21.0394, Lng: 105.9364} // about 10 km east of 00004
	listings := []struct {
		price int64
		days  int
	}{{1_000_000, 2}, {1_100_000, 6}, {1_200_000, 10}, {1_300_000, 0}, {1_400_000, 0}, {1_500_000, 0}}
	samples := make([]catalogsvc.PriceSample, 0, len(listings)+1)
	for _, s := range listings {
		id := catalogtest.Listing(t, e.db, seller, func(d *catalogdomain.ListingInput, r *catalogdomain.Refs) {
			d.Price, r.CommunePoint = s.price, far
		})
		samples = append(samples, catalogsvc.PriceSample{
			ListingID: id, Price: s.price, Sold: s.days > 0, DaysToSell: s.days, Point: far,
		})
	}
	gone := uuid.NewV7() // in the sample, deleted before the summaries
	samples = append(samples, catalogsvc.PriceSample{ListingID: gone, Price: 1_600_000})
	cat.samples = samples
	e.watchers.rows = []discoverysvc.Watcher{{PriceMax: 1_250_000}, {OffersOnly: true}}
	photo := mediatest.Upload(t, e.media, me, "image/jpeg", mediatest.JPEG(t, 96, 64))
	in := bike()
	in.Photos = []uuid.UUID{photo}

	v, err := e.assist.PriceGuide(ctx, me, in)
	if err != nil {
		t.Fatal(err)
	}
	// Weights 2 2 2 1 1 1 1 sum to 10; the running weight reaches 5 at 1.2.
	if v.Estimate == nil || v.Estimate.Median != 1_200_000 || v.Estimate.SampleSize != 7 {
		t.Fatalf("estimate %+v", v.Estimate)
	}
	if want := []domain.Suggestion{
		{Kind: domain.SuggestFast, Price: 1_050_000, Days: 4}, {Kind: domain.SuggestMarket, Price: 1_200_000, Days: 6},
	}; !slices.Equal(v.Suggestions, want) {
		t.Fatalf("suggestions %+v", v.Suggestions)
	}
	if len(v.Similar) != 6 || v.Similar[0].Price != 1_000_000 || !v.Similar[0].Sold || v.Similar[0].DaysToSell == nil ||
		*v.Similar[0].DaysToSell != 2 || v.Similar[3].DaysToSell != nil || v.Similar[0].Km < 9 || v.Similar[0].Km > 11 ||
		v.Similar[0].Title != "Xe đạp Asama" || v.Similar[0].CoverURL == "" {
		t.Fatalf("similar %+v", v.Similar)
	}
	if !slices.Equal(v.SoldPrices, []int64{1_000_000, 1_100_000, 1_200_000}) ||
		!slices.Equal(v.LivePrices, []int64{1_300_000, 1_400_000, 1_500_000, 1_600_000}) {
		t.Fatalf("prices %v %v", v.SoldPrices, v.LivePrices)
	}
	if len(v.Watchers) != 2 || v.Watchers[0].PriceMax != 1_250_000 || !v.Watchers[1].OffersOnly {
		t.Fatalf("watchers %+v", v.Watchers)
	}
	e.watchers.mu.Lock()
	asked, who := e.watchers.asked, e.watchers.who
	e.watchers.mu.Unlock()
	if who != me || asked.CategoryCode != "vehicles.bicycle" || asked.Point.Lat != 21.0394 ||
		asked.Text != "Xe đạp Asama\nĐi ít\nKhung: nhôm" {
		t.Fatalf("watchers asked %v %+v", who, asked)
	}
	cat.mu.Lock()
	q := cat.asked
	cat.mu.Unlock()
	if q.Exclude != me || len(q.Text) != catalogdomain.TextDims || len(q.Photo) != catalogdomain.PhotoDims {
		t.Fatalf("sample asked %+v", q)
	}
}

// Under 5 similar listings there is no range and no suggestion; the
// listings found and the watchers still answer.
func TestPriceGuideFewListings(t *testing.T) {
	t.Parallel()
	var cat *sampleOf
	e := newEnv(t, func(x *env) {
		cat = &sampleOf{Service: x.deps.Catalog}
		x.deps.Catalog = cat
	})
	seller, me := accounttest.New(t, e.db), accounttest.New(t, e.db)
	id := catalogtest.Listing(t, e.db, seller)
	cat.samples = []catalogsvc.PriceSample{{ListingID: id, Price: 1_500_000, Sold: true, DaysToSell: 3}}
	e.watchers.rows = []discoverysvc.Watcher{{}}
	v, err := e.assist.PriceGuide(context.Background(), me, bike())
	if err != nil {
		t.Fatal(err)
	}
	if v.Estimate != nil || len(v.Suggestions) != 0 || len(v.Similar) != 1 || len(v.Watchers) != 1 ||
		!slices.Equal(v.SoldPrices, []int64{1_500_000}) || v.LivePrices == nil {
		t.Fatalf("%+v", v)
	}
}

// Embedding models that are down give a guide without a comparison; the
// watchers still answer.
func TestPriceGuideWithoutEmbeddings(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(x *env) { x.deps.Text, x.deps.Image = failingEmbed{}, failingEmbed{} })
	e.watchers.rows = []discoverysvc.Watcher{{}}
	v, err := e.assist.PriceGuide(context.Background(), accounttest.New(t, e.db), bike())
	if err != nil || v.Estimate != nil || len(v.Similar) != 0 || len(v.Watchers) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestPriceGuideRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	me, other := accounttest.New(t, e.db), accounttest.New(t, e.db)

	in := bike()
	in.CategoryCode = "vehicles"
	_, err := e.assist.PriceGuide(ctx, me, in)
	if a, ok := errors.AsType[*apperr.Error](err); !ok || a.Code != "invalid_listing" || a.Fields["category_code"] == "" {
		t.Fatalf("a root category: %v", err)
	}
	in = bike()
	in.CommuneCode = "99999"
	_, err = e.assist.PriceGuide(ctx, me, in)
	if a, ok := errors.AsType[*apperr.Error](err); !ok || a.Fields["commune_code"] == "" {
		t.Fatalf("an unknown commune: %v", err)
	}
	in = bike()
	in.Photos = []uuid.UUID{mediatest.Upload(t, e.media, other, "image/jpeg", mediatest.JPEG(t, 96, 64))}
	if _, err = e.assist.PriceGuide(ctx, me, in); !errors.Is(err, mediadomain.ErrInvalidUpload) {
		t.Fatalf("someone else's photo: %v", err)
	}
	suspended := accounttest.New(t, e.db)
	accounttest.Suspend(t, e.accounts, suspended)
	if _, err = e.assist.PriceGuide(ctx, suspended, bike()); err == nil {
		t.Fatal("a suspended account")
	}
}

// A seller blocked either way stays out of the similar listings, as out of
// every search; their prices still count in the range.
func TestPriceGuideLeavesBlockedSellersOut(t *testing.T) {
	t.Parallel()
	var cat *sampleOf
	e := newEnv(t, func(x *env) {
		cat = &sampleOf{Service: x.deps.Catalog}
		x.deps.Catalog = cat
	})
	ctx := context.Background()
	me, blocked, blocker, other := accounttest.New(t, e.db), accounttest.New(t, e.db), accounttest.New(t, e.db), accounttest.New(t, e.db)
	for _, seller := range []uuid.UUID{blocked, blocker, other} {
		cat.samples = append(cat.samples, catalogsvc.PriceSample{ListingID: catalogtest.Listing(t, e.db, seller), Price: 1_500_000})
	}
	if err := e.accounts.Block(ctx, me, blocked); err != nil {
		t.Fatal(err)
	}
	if err := e.accounts.Block(ctx, blocker, me); err != nil {
		t.Fatal(err)
	}
	v, err := e.assist.PriceGuide(ctx, me, bike())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Similar) != 1 || v.Similar[0].ListingID != cat.samples[2].ListingID || len(v.LivePrices) != 3 {
		t.Fatalf("%+v", v)
	}
}
