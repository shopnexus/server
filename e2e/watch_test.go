package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"uuid"
)

type searchWatch struct {
	ID       string `json:"id"`
	Query    string `json:"query"`
	NewCount int    `json:"new_count"`
	Alert    bool   `json:"alert"`
	Area     *struct {
		CommuneCode string `json:"commune_code"`
		Name        string `json:"name"`
	} `json:"area"`
}

// The sweep runs 30 minutes after a watch is made, so no test here waits for
// its notice; the discovery service and notification worker tests cover it.
func TestSearchWatches(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	in := map[string]any{"q": "xe đạp", "price_max": 3_000_000, "near_commune": "00004", "radius_km": 5}
	created := expect[searchWatch](t, call(t, "POST", "/v1/search/watches", in, bearer(me.AccessToken)), http.StatusCreated)
	if created.Area == nil || created.Area.CommuneCode != "00004" || !created.Alert {
		t.Fatalf("%+v", created)
	}
	again := expect[searchWatch](t, call(t, "POST", "/v1/search/watches", map[string]any{"q": "Xe Dap"}, bearer(me.AccessToken)), http.StatusOK)
	if again.ID != created.ID || again.Area != nil {
		t.Fatalf("the same words take the new filters: %+v", again)
	}
	list := expect[struct {
		Items []searchWatch `json:"items"`
	}](t, call(t, "GET", "/v1/search/watches", nil, bearer(me.AccessToken)), http.StatusOK)
	if len(list.Items) != 1 {
		t.Fatalf("%+v", list)
	}
	res := call(t, "GET", "/v1/search?q=xe+dap", nil, bearer(me.AccessToken))
	if v := expect[struct {
		WatchID string `json:"watch_id"`
	}](t, res, http.StatusOK); v.WatchID != created.ID {
		t.Fatalf("watch_id: %s", res.Body)
	}
	expectStatus(t, call(t, "GET", "/v1/search?watch="+created.ID, nil, bearer(me.AccessToken)), http.StatusOK)
	base := "/v1/search/watches/" + created.ID
	check(t, "mute", call(t, "POST", base+"/mute", nil, bearer(me.AccessToken)), 204, "")
	check(t, "unmute", call(t, "POST", base+"/unmute", nil, bearer(me.AccessToken)), 204, "")
	other, _ := newAccount(t)
	check(t, "mute someone else's", call(t, "POST", base+"/mute", nil, bearer(other.AccessToken)), 404, "watch_not_found")
	check(t, "unmute someone else's", call(t, "POST", base+"/unmute", nil, bearer(other.AccessToken)), 404, "watch_not_found")
	check(t, "search someone else's", call(t, "GET", "/v1/search?watch="+created.ID, nil, bearer(other.AccessToken)), 404, "watch_not_found")
	check(t, "search a watch with q", call(t, "GET", "/v1/search?q=x&watch="+created.ID, nil, bearer(me.AccessToken)), 400, "search_conflict")
	check(t, "mute a bad id", call(t, "POST", "/v1/search/watches/x/mute", nil, bearer(me.AccessToken)), 400, "")
	check(t, "unmute a bad id", call(t, "POST", "/v1/search/watches/x/unmute", nil, bearer(me.AccessToken)), 400, "")
	check(t, "mute signed out", call(t, "POST", base+"/mute", nil), 401, "unauthenticated")
	check(t, "unmute signed out", call(t, "POST", base+"/unmute", nil), 401, "unauthenticated")
	check(t, "delete", call(t, "DELETE", base, nil, bearer(me.AccessToken)), 204, "")
	check(t, "delete again", call(t, "DELETE", base, nil, bearer(me.AccessToken)), 204, "")
	check(t, "delete a bad id", call(t, "DELETE", "/v1/search/watches/x", nil, bearer(me.AccessToken)), 400, "")
	check(t, "delete signed out", call(t, "DELETE", base, nil), 401, "unauthenticated")

	check(t, "no q", call(t, "POST", "/v1/search/watches", map[string]any{"q": "  "}, bearer(me.AccessToken)), 400, "")
	check(t, "price range", call(t, "POST", "/v1/search/watches", map[string]any{"q": "a", "price_min": 5, "price_max": 1}, bearer(me.AccessToken)), 400, "invalid_price_range")
	check(t, "abroad", call(t, "POST", "/v1/search/watches", map[string]any{"q": "a", "near_lat": 48.85, "near_lng": 2.35}, bearer(me.AccessToken)), 400, "invalid_position")
	check(t, "radius, no place", call(t, "POST", "/v1/search/watches", map[string]any{"q": "a", "radius_km": 5}, bearer(me.AccessToken)), 400, "position_required")
	check(t, "create signed out", call(t, "POST", "/v1/search/watches", map[string]any{"q": "a"}), 401, "unauthenticated")
	check(t, "list signed out", call(t, "GET", "/v1/search/watches", nil), 401, "unauthenticated")
	for i := range 20 {
		expectStatus(t, call(t, "POST", "/v1/search/watches", map[string]any{"q": fmt.Sprintf("tìm %d %s", i, uuid.NewV7())}, bearer(me.AccessToken)), http.StatusCreated)
	}
	check(t, "the 21st", call(t, "POST", "/v1/search/watches", map[string]any{"q": "một nữa"}, bearer(me.AccessToken)), 409, "watch_limit_reached")
}
