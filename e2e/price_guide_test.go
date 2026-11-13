package e2e

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

type guideWatcher struct {
	PriceMax   int64 `json:"price_max"`
	OffersOnly bool  `json:"offers_only"`
}

type priceGuide struct {
	Estimate *struct {
		Median     int64  `json:"median"`
		Source     string `json:"source"`
		SampleSize int    `json:"sample_size"`
	} `json:"estimate"`
	Suggestions []struct {
		Kind  string `json:"kind"`
		Price int64  `json:"price"`
		Days  int    `json:"days"`
	} `json:"suggestions"`
	Similar []struct {
		ListingID  string `json:"listing_id"`
		Sold       bool   `json:"sold"`
		DaysToSell *int   `json:"days_to_sell"`
	} `json:"similar"`
	SoldPrices []int64        `json:"sold_prices"`
	LivePrices []int64        `json:"live_prices"`
	Watchers   []guideWatcher `json:"watchers"`
}

// Six listings with the guide's own text, three sold after 2, 6 and 10
// days; a buyer's watch for its words; a third account prices the draft.
func TestPriceGuide(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	me, _ := newAccount(t)
	word := "guide" + uuid.NewV7().String()[:8] // a word no other test's listing holds
	title := "Ghế xếp " + word
	ids := make([]string, 0, 6)
	for _, price := range []int64{1_000_000, 1_100_000, 1_200_000, 1_300_000, 1_400_000, 1_500_000} {
		fields := map[string]any{"title": title, "category_code": "furniture.other", "price": price}
		ids = append(ids, activeListing(t, seller.AccessToken, fields).ID)
	}
	for i, days := range []int{2, 6, 10} {
		expectStatus(t, call(t, "POST", "/v1/listings/"+ids[i]+"/mark-sold", nil, bearer(seller.AccessToken)), http.StatusOK)
		if _, err := db.Exec(context.Background(), `UPDATE listings SET published_at = sold_at - make_interval(days => @days)
WHERE id = @id`, pgx.StrictNamedArgs{"id": ids[i], "days": days}); err != nil {
			t.Fatal(err)
		}
	}
	testenv.Eventually(t, 30*time.Second, func() error {
		var n int
		if err := db.QueryRow(context.Background(), `SELECT count(*) FROM listing_text_vectors WHERE listing_id = ANY(@ids::uuid[])`,
			pgx.StrictNamedArgs{"ids": ids}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n < len(ids) {
			return fmt.Errorf("%d of %d text vectors", n, len(ids))
		}
		return nil
	})
	w := expect[searchWatch](t, call(t, "POST", "/v1/search/watches",
		map[string]any{"q": "ghế " + word, "price_max": 1_250_000}, bearer(buyer.AccessToken)), http.StatusCreated)
	// The model's phrases for an unknown word are not this test's subject.
	if _, err := db.Exec(context.Background(), `UPDATE search_watches SET phrases = jsonb_build_array(@word::text) WHERE id = @id`,
		pgx.StrictNamedArgs{"word": word, "id": w.ID}); err != nil {
		t.Fatal(err)
	}

	draft := map[string]any{
		"title": title, "category_code": "furniture.other", "condition": "used", "description": "Đi ít",
		"commune_code": "00004",
	}
	g := expect[priceGuide](t, call(t, "POST", "/v1/price-guides", draft, bearer(me.AccessToken)), http.StatusOK)
	if g.Estimate == nil || g.Estimate.Source != "market" || g.Estimate.Median != 1_200_000 || g.Estimate.SampleSize != 6 {
		t.Fatalf("estimate %+v", g.Estimate)
	}
	if len(g.Suggestions) != 2 || g.Suggestions[0].Kind != "fast" || g.Suggestions[0].Price != 1_050_000 ||
		g.Suggestions[0].Days != 4 || g.Suggestions[1].Price != 1_200_000 || g.Suggestions[1].Days != 6 {
		t.Fatalf("suggestions %+v", g.Suggestions)
	}
	if len(g.Similar) != 6 || len(g.SoldPrices) != 3 || len(g.LivePrices) != 3 {
		t.Fatalf("similar %+v, sold %v, live %v", g.Similar, g.SoldPrices, g.LivePrices)
	}
	// Other tests' watches share the database; ours is the one at 1,250,000.
	if !slices.ContainsFunc(g.Watchers, func(w guideWatcher) bool { return w.PriceMax == 1_250_000 }) {
		t.Fatalf("watchers %+v", g.Watchers)
	}
}

func TestPriceGuideRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	other, _ := newAccount(t)
	draft := func(change map[string]any) map[string]any {
		d := map[string]any{"title": "Xe đạp Asama", "category_code": "vehicles.bicycle", "condition": "used", "commune_code": "00004"}
		maps.Copy(d, change)
		return d
	}
	post := func(token string, body map[string]any) Response {
		return call(t, "POST", "/v1/price-guides", body, bearer(token))
	}
	check(t, "signed out", call(t, "POST", "/v1/price-guides", draft(nil)), 401, "unauthenticated")
	check(t, "no title", post(me.AccessToken, draft(map[string]any{"title": ""})), 400, "invalid_request")
	check(t, "a root category", post(me.AccessToken, draft(map[string]any{"category_code": "vehicles"})), 422, "invalid_listing")
	check(t, "someone else's photo", post(me.AccessToken,
		draft(map[string]any{"photos": []string{upload(t, other.AccessToken, photoWithGPS(t))}})), 422, "invalid_upload")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "suspended", post(suspended.AccessToken, draft(nil)), 403, "account_suspended")
	expectStatus(t, post(me.AccessToken, draft(nil)), http.StatusOK)
}
