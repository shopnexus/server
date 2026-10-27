package e2e

import (
	"maps"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type searchView struct {
	Items []struct {
		ID         string `json:"id"`
		DistanceKm *int   `json:"distance_km"`
		Saved      bool   `json:"saved"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
	Understood string `json:"understood"`
	Chips      []struct {
		ID string `json:"id"`
	} `json:"chips"`
	Counts []struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	} `json:"counts"`
}

// postActive posts an active listing with the given fields over the
// default body and waits until it is on sale.
func postActive(t *testing.T, token string, fields map[string]any) listing {
	t.Helper()
	body := draftWith(upload(t, token, photoWithGPS(t)))
	maps.Copy(body, fields)
	res := call(t, server, "POST", "/v1/listings", body, bearer(token), newKey())
	if res.Status != http.StatusCreated {
		t.Fatalf("post: %d %s", res.Status, res.Body)
	}
	l := settled(t, token, decode[listing](t, res).ID)
	if l.Status != "active" {
		t.Fatalf("%+v", l)
	}
	return l
}

// band is a price range of the test's own, so its search sees only its
// listings while other tests post theirs.
func band() (lo, hi int64) {
	lo = int64(2_000_000 + rand.IntN(400_000)*1000)
	return lo, lo + 2000
}

func search(t *testing.T, query url.Values, headers ...[2]string) (Response, searchView) {
	t.Helper()
	res := call(t, server, "GET", "/v1/search?"+query.Encode(), nil, headers...)
	if res.Status != http.StatusOK {
		return res, searchView{}
	}
	return res, decode[searchView](t, res)
}

func TestSearch(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	lo, hi := band()
	bike := postActive(t, seller.AccessToken, map[string]any{"title": "Xe đạp đi làm Asama", "price": lo})
	lamp := postActive(t, seller.AccessToken, map[string]any{"title": "Đèn xe đạp", "category_code": "sports.other", "price": hi})
	const q = "xe dap di lam duoi 3tr"
	query := url.Values{"q": {q}, "near_commune": {"00004"}, "price_min": {strconv.FormatInt(lo, 10)}, "price_max": {strconv.FormatInt(hi, 10)}}

	res, v := search(t, query)
	if res.Status != http.StatusOK || len(v.Items) != 2 || v.Items[0].ID != bike.ID || v.Items[1].ID != lamp.ID ||
		v.Understood == "" || len(v.Chips) == 0 {
		t.Fatalf("understood: %d %s", res.Status, res.Body)
	}
	for _, it := range v.Items {
		if it.DistanceKm == nil || *it.DistanceKm < 0 {
			t.Fatalf("distance: %s", res.Body)
		}
	}
	if strings.Contains(string(res.Body), `"lat"`) || strings.Contains(string(res.Body), `"lng"`) {
		t.Fatalf("a point leaked: %s", res.Body)
	}
	calls := model.asked()
	if _, v = search(t, query); model.asked() != calls || len(v.Chips) == 0 {
		t.Fatalf("the same query called the model again: %d → %d", calls, model.asked())
	}
	off := url.Values{}
	maps.Copy(off, query)
	off.Add("off", "category-0")
	if _, v = search(t, off); model.asked() != calls || len(v.Items) != 2 {
		t.Fatalf("off: %d %+v", model.asked(), v)
	}
	for _, c := range v.Chips {
		if c.ID == "category-0" {
			t.Fatalf("the chip turned off is back: %+v", v.Chips)
		}
	}

	buyer, _ := newAccount(t)
	if r := call(t, server, "PUT", "/v1/saved/"+bike.ID, nil, bearer(buyer.AccessToken)); r.Status != http.StatusNoContent {
		t.Fatalf("save: %d %s", r.Status, r.Body)
	}
	_, v = search(t, query, bearer(buyer.AccessToken))
	if len(v.Items) != 2 || !v.Items[0].Saved || v.Items[1].Saved {
		t.Fatalf("saved flags: %+v", v)
	}
}

func TestSearchCategory(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	lo, hi := band()
	postActive(t, seller.AccessToken, map[string]any{"price": lo})
	_, v := search(t, url.Values{"category": {"vehicles"}, "price_min": {strconv.FormatInt(lo, 10)}, "price_max": {strconv.FormatInt(hi, 10)}})
	if len(v.Items) != 1 || len(v.Counts) != 1 || v.Counts[0].Category != "vehicles.bicycle" || v.Counts[0].Count != 1 {
		t.Fatalf("%+v", v)
	}
	res, v := search(t, url.Values{
		"q": {"xe dap di lam duoi 3tr"}, "category": {"vehicles"},
		"price_min": {strconv.FormatInt(lo, 10)}, "price_max": {strconv.FormatInt(hi, 10)},
	})
	if res.Status != http.StatusOK || len(v.Counts) != 1 || len(v.Chips) == 0 {
		t.Fatalf("text on a root category page: %d %s", res.Status, res.Body)
	}
}

func TestSearchRefusals(t *testing.T) {
	t.Parallel()
	expect := func(name string, query url.Values, code string) {
		t.Helper()
		res, _ := search(t, query)
		if res.Status != http.StatusBadRequest || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	expect("nearest without a position", url.Values{"sort": {"nearest"}}, "position_required")
	expect("a latitude out of range", url.Values{"near_lat": {"95"}, "near_lng": {"1"}}, "")
	expect("a latitude alone", url.Values{"near_lat": {"10.7"}}, "invalid_position")
	expect("an unknown commune", url.Values{"near_commune": {"nope"}}, "invalid_position")
	expect("a word for a cursor", url.Values{"cursor": {"x"}}, "invalid_cursor")
	expect("an unknown sort", url.Values{"sort": {"cheapest"}}, "")
	expect("a minimum above the maximum", url.Values{"price_min": {"5000000"}, "price_max": {"1000000"}}, "invalid_price_range")
	if res, v := search(t, url.Values{"q": {"?"}}); res.Status != http.StatusOK || len(v.Items) != 0 {
		t.Errorf("punctuation: %d %s", res.Status, res.Body)
	}
}

func TestNearbyFeed(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	postActive(t, seller.AccessToken, nil)
	if res := call(t, server, "GET", "/v1/feed/nearby", nil); res.Status != http.StatusBadRequest || errorOf(t, res).Code != "position_required" {
		t.Fatalf("no position: %d %s", res.Status, res.Body)
	}
	res := call(t, server, "GET", "/v1/feed/nearby?near_commune=00004", nil)
	v := decode[searchView](t, res)
	if res.Status != http.StatusOK || len(v.Items) == 0 || v.Items[0].DistanceKm == nil {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
}
