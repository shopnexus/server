package e2e

import (
	"net/http"
	"net/url"
	"testing"
)

type historyView struct {
	Items []struct {
		ID    string `json:"id"`
		Query string `json:"query"`
		Title string `json:"title"`
	} `json:"items"`
}

func TestSearchHistory(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	search(t, url.Values{"q": {"xe đạp"}}, bearer(me.AccessToken))
	search(t, url.Values{"q": {"Xe  Dap"}}, bearer(me.AccessToken))
	search(t, url.Values{"q": {"tủ lạnh"}}, bearer(me.AccessToken))

	res := call(t, "GET", "/v1/search/history", nil, bearer(me.AccessToken))
	h := decode[historyView](t, res)
	if res.Status != http.StatusOK || len(h.Items) != 2 || h.Items[0].Query != "tủ lạnh" || h.Items[1].Query != "Xe  Dap" ||
		h.Items[0].Title != "Tủ lạnh" {
		t.Fatalf("history: %d %s", res.Status, res.Body)
	}
	check(t, "forget one", call(t, "DELETE", "/v1/search/history/"+h.Items[0].ID, nil, bearer(me.AccessToken)), 204, "")
	check(t, "forget a bad id", call(t, "DELETE", "/v1/search/history/x", nil, bearer(me.AccessToken)), 400, "")
	check(t, "forget signed out", call(t, "DELETE", "/v1/search/history/"+h.Items[0].ID, nil), 401, "unauthenticated")
	check(t, "clear", call(t, "DELETE", "/v1/search/history", nil, bearer(me.AccessToken)), 204, "")
	check(t, "clear signed out", call(t, "DELETE", "/v1/search/history", nil), 401, "unauthenticated")
	check(t, "read signed out", call(t, "GET", "/v1/search/history", nil), 401, "unauthenticated")
	if h = decode[historyView](t, call(t, "GET", "/v1/search/history", nil, bearer(me.AccessToken))); len(h.Items) != 0 {
		t.Fatalf("after clear: %+v", h)
	}
}
