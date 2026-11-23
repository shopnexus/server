package e2e

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
	"uuid"
)

// searchIDs are the IDs of the first page of a search token makes.
func searchIDs(t *testing.T, token, query string) []string {
	t.Helper()
	res := call(t, "GET", "/v1/search?"+query, nil, bearer(token))
	got := expect[searchView](t, res, http.StatusOK)
	out := make([]string, len(got.Items))
	for i := range got.Items {
		out[i] = got.Items[i].ID
	}
	return out
}

func TestHiddenListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	me, _ := newAccount(t)
	word := "hidden" + uuid.NewV7().String()[:8]
	l := activeListing(t, seller.AccessToken, map[string]any{"title": "Ghế gỗ " + word})
	q := "q=" + url.QueryEscape(word)
	if !slices.Contains(searchIDs(t, me.AccessToken, q), l.ID) {
		t.Fatal("found before hiding")
	}
	path := "/v1/hidden/" + l.ID
	check(t, "hide", call(t, "PUT", path, nil, bearer(me.AccessToken)), 204, "")
	check(t, "hide again", call(t, "PUT", path, nil, bearer(me.AccessToken)), 204, "")
	if slices.Contains(searchIDs(t, me.AccessToken, q), l.ID) {
		t.Fatal("hidden")
	}
	check(t, "unhide", call(t, "DELETE", path, nil, bearer(me.AccessToken)), 204, "")
	if !slices.Contains(searchIDs(t, me.AccessToken, q), l.ID) {
		t.Fatal("back after unhide")
	}
	check(t, "hide own", call(t, "PUT", path, nil, bearer(seller.AccessToken)), 409, "own_listing")
	check(t, "hide unknown", call(t, "PUT", "/v1/hidden/"+uuid.NewV7().String(), nil, bearer(me.AccessToken)), 404, "listing_not_found")
	check(t, "hide a bad id", call(t, "PUT", "/v1/hidden/x", nil, bearer(me.AccessToken)), 400, "")
	check(t, "hide signed out", call(t, "PUT", path, nil), 401, "unauthenticated")
	check(t, "unhide a bad id", call(t, "DELETE", "/v1/hidden/x", nil, bearer(me.AccessToken)), 400, "")
	check(t, "unhide signed out", call(t, "DELETE", path, nil), 401, "unauthenticated")

	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": seller.Account.ID}, bearer(me.AccessToken)), http.StatusNoContent)
	if slices.Contains(searchIDs(t, me.AccessToken, q), l.ID) {
		t.Fatal("a blocked seller's listing")
	}
}
