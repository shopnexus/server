package e2e

import (
	"maps"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// searchCard is a SearchItem, a listing as search, Home's nearby feed and
// the seller view show it; a nil DistanceKm is an absent key.
type searchCard struct {
	ID            string `json:"id"`
	SellerID      string `json:"seller_id"`
	Title         string `json:"title"`
	CategoryCode  string `json:"category_code"`
	CoverThumbURL string `json:"cover_thumb_url"`
	Commune       string `json:"commune"`
	CommuneCode   string `json:"commune_code"`
	DistanceKm    *int   `json:"distance_km"`
	Saved         bool   `json:"saved"`
}

type searchView struct {
	Items      []searchCard `json:"items"`
	NextCursor string       `json:"next_cursor"`
	Chips      []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"chips"`
	PreviewChips []struct {
		Label string `json:"label"`
	} `json:"preview_chips"`
}

// band is a price range of the test's own, so its search sees only its
// listings while other tests post theirs.
func band() (lo, hi int64) {
	lo = int64(2_000_000 + rand.IntN(400_000)*1000)
	return lo, lo + 2000
}

// search runs a search; a guest's comes from a fresh address, so the guest
// rate limit never fills across tests.
func search(t *testing.T, query url.Values, headers ...[2]string) (Response, searchView) {
	t.Helper()
	if !slices.ContainsFunc(headers, func(h [2]string) bool { return h[0] == "Authorization" }) {
		headers = append(headers, newClient())
	}
	res := call(t, "GET", "/v1/search?"+query.Encode(), nil, headers...)
	if res.Status != http.StatusOK {
		return res, searchView{}
	}
	return res, decode[searchView](t, res)
}

func TestSearch(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	lo, hi := band()
	bike := activeListing(t, seller.AccessToken, map[string]any{"title": "Xe đạp đi làm Asama", "price": lo})
	lamp := activeListing(t, seller.AccessToken, map[string]any{"title": "Đèn xe đạp", "category_code": "sports.other", "price": hi})
	const q = "xe dap di lam duoi 3tr"
	query := url.Values{"q": {q}, "near_commune": {"00004"}, "price_min": {strconv.FormatInt(lo, 10)}, "price_max": {strconv.FormatInt(hi, 10)}}

	// A guest's query skips the model.
	calls := model.asked()
	if _, v := search(t, query); len(v.Items) != 2 || len(v.Chips) != 0 || model.asked() != calls {
		t.Fatalf("guest: %d calls, %+v", model.asked()-calls, v)
	}
	buyer, _ := newAccount(t)
	me := bearer(buyer.AccessToken)
	res, v := search(t, query, me)
	if res.Status != http.StatusOK || len(v.Items) != 2 || v.Items[0].ID != bike.ID || v.Items[1].ID != lamp.ID ||
		len(v.Chips) == 0 {
		t.Fatalf("read by the model: %d %s", res.Status, res.Body)
	}
	for _, it := range v.Items {
		if it.DistanceKm == nil || *it.DistanceKm < 0 || it.SellerID != seller.Account.ID {
			t.Fatalf("distance and seller: %s", res.Body)
		}
	}
	if v.Items[0].CategoryCode != "vehicles.bicycle" || v.Items[1].CategoryCode != "sports.other" {
		t.Fatalf("categories: %s", res.Body)
	}
	if strings.Contains(string(res.Body), `"lat"`) || strings.Contains(string(res.Body), `"lng"`) {
		t.Fatalf("a point leaked: %s", res.Body)
	}
	// Now that the answer is cached, a guest's first page previews its chips unapplied.
	if _, g := search(t, query); len(g.Chips) != 0 || len(g.PreviewChips) == 0 {
		t.Fatalf("guest preview: %+v", g)
	}
	calls = model.asked()
	if _, v = search(t, query, me); model.asked() != calls || len(v.Chips) == 0 {
		t.Fatalf("the same query called the model again: %d → %d", calls, model.asked())
	}
	// The chips follow the request's language; the phrase stays as the
	// model wrote it.
	res, v = search(t, query, me, [2]string{"Accept-Language", "en"})
	labels := make([]string, 0, len(v.Chips))
	for _, c := range v.Chips {
		labels = append(labels, c.Label)
	}
	if model.asked() != calls || !slices.Equal(labels, []string{"xe đạp đi làm", "Bicycles", "Under 3M ₫"}) ||
		res.Header.Get("Vary") != "Accept-Language" {
		t.Fatalf("English chips %q, Vary %q", labels, res.Header.Get("Vary"))
	}
	off := url.Values{}
	maps.Copy(off, query)
	off.Add("off", "category-0")
	if _, v = search(t, off, me); model.asked() != calls || len(v.Items) != 2 {
		t.Fatalf("off: %d %+v", model.asked(), v)
	}
	for _, c := range v.Chips {
		if c.ID == "category-0" {
			t.Fatalf("the chip turned off is back: %+v", v.Chips)
		}
	}

	expectStatus(t, call(t, "PUT", "/v1/saved/"+bike.ID, nil, me), http.StatusNoContent)
	_, v = search(t, query, me)
	if len(v.Items) != 2 || !v.Items[0].Saved || v.Items[1].Saved {
		t.Fatalf("saved flags: %+v", v)
	}
}

// One guest address gets a burst of 30 searches with q; browsing and a
// signed-in caller from it are not limited.
func TestSearchLimitsAGuest(t *testing.T) {
	t.Parallel()
	from := newClient()
	for i := range 60 {
		res := call(t, "GET", "/v1/search?q=xe", nil, from)
		if res.Status == http.StatusTooManyRequests {
			check(t, "past the burst", res, http.StatusTooManyRequests, "rate_limited")
			expectStatus(t, call(t, "GET", "/v1/search?category=vehicles", nil, from), http.StatusOK)
			me, _ := newAccount(t)
			expectStatus(t, call(t, "GET", "/v1/search?q=xe", nil, from, bearer(me.AccessToken)), http.StatusOK)
			return
		}
		if i == 59 {
			t.Fatal("60 guest searches with q in a row were not rate limited")
		}
	}
}

func TestSearchCategory(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	lo, hi := band()
	activeListing(t, seller.AccessToken, map[string]any{"price": lo})
	_, v := search(t, url.Values{"category": {"vehicles"}, "price_min": {strconv.FormatInt(lo, 10)}, "price_max": {strconv.FormatInt(hi, 10)}})
	if len(v.Items) != 1 {
		t.Fatalf("%+v", v)
	}
	buyer, _ := newAccount(t)
	res, v := search(t, url.Values{
		"q": {"xe dap di lam duoi 3tr"}, "category": {"vehicles"},
		"price_min": {strconv.FormatInt(lo, 10)}, "price_max": {strconv.FormatInt(hi, 10)},
	}, bearer(buyer.AccessToken))
	if res.Status != http.StatusOK || len(v.Chips) == 0 {
		t.Fatalf("text on a root category page: %d %s", res.Status, res.Body)
	}
}

func TestSearchRefusals(t *testing.T) {
	t.Parallel()
	refused := func(name string, query url.Values, code string) {
		t.Helper()
		res, _ := search(t, query)
		check(t, name, res, http.StatusBadRequest, code)
	}
	refused("nearest without a position", url.Values{"sort": {"nearest"}}, "position_required")
	refused("a latitude out of range", url.Values{"near_lat": {"95"}, "near_lng": {"1"}}, "")
	refused("a latitude alone", url.Values{"near_lat": {"10.7"}}, "invalid_position")
	refused("an unknown commune", url.Values{"near_commune": {"nope"}}, "invalid_position")
	refused("a word for a cursor", url.Values{"cursor": {"x"}}, "invalid_cursor")
	refused("an unknown sort", url.Values{"sort": {"cheapest"}}, "")
	refused("a minimum above the maximum", url.Values{"price_min": {"5000000"}, "price_max": {"1000000"}}, "invalid_price_range")
	refused("a malformed province", url.Values{"province": {"1"}}, "invalid_request")
	refused("an unknown province", url.Values{"province": {"99"}}, "invalid_province")
	if res, v := search(t, url.Values{"q": {"?"}}); res.Status != http.StatusOK || len(v.Items) != 0 {
		t.Errorf("punctuation: %d %s", res.Status, res.Body)
	}
}

func TestSearchNearby(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	activeListing(t, seller.AccessToken, nil)
	if res := call(t, "GET", "/v1/search?sort=nearby", nil, newClient()); res.Status != http.StatusBadRequest || errorOf(t, res).Code != "position_required" {
		t.Fatalf("no position: %d %s", res.Status, res.Body)
	}
	if res := call(t, "GET", "/v1/search?sort=nearby&near_commune=00004&q=gh%E1%BA%BF", nil, newClient()); res.Status != http.StatusBadRequest || errorOf(t, res).Code != "search_conflict" {
		t.Fatalf("with q: %d %s", res.Status, res.Body)
	}
	res := call(t, "GET", "/v1/search?sort=nearby&near_commune=00004", nil, newClient())
	v := decode[searchView](t, res)
	if res.Status != http.StatusOK || len(v.Items) == 0 || v.Items[0].DistanceKm == nil {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
}
