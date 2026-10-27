package e2e

import (
	"net/http"
	"testing"
	"uuid"
)

type savedList struct {
	Items []struct {
		ListingID  string `json:"listing_id"`
		SavedPrice int64  `json:"saved_price"`
		Listing    struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"listing"`
	} `json:"items"`
}

func TestSaved(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	seller, _ := newAccount(t)
	me, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)
	path := "/v1/saved/" + l.ID

	expect("save", call(t, server, "PUT", path, nil, bearer(me.AccessToken)), 204, "")
	expect("save again", call(t, server, "PUT", path, nil, bearer(me.AccessToken)), 204, "")
	res := call(t, server, "GET", "/v1/saved", nil, bearer(me.AccessToken))
	if got := decode[savedList](t, res); res.Status != http.StatusOK || len(got.Items) != 1 || got.Items[0].Listing.Title == "" {
		t.Fatalf("list: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(seller.AccessToken)); res.Status != http.StatusOK {
		t.Fatalf("hide: %d", res.Status)
	}
	res = call(t, server, "GET", "/v1/saved", nil, bearer(me.AccessToken))
	if got := decode[savedList](t, res); got.Items[0].Listing.Status != "hidden" || got.Items[0].Listing.Title != "" {
		t.Fatalf("hidden: %s", res.Body)
	}
	expect("save a hidden listing", call(t, server, "PUT", "/v1/saved/"+activeHidden(t, seller.AccessToken), nil, bearer(me.AccessToken)), 409, "listing_unavailable")
	expect("save own", call(t, server, "PUT", "/v1/saved/"+activeListing(t, seller.AccessToken).ID, nil, bearer(seller.AccessToken)), 409, "own_listing")
	expect("save unknown", call(t, server, "PUT", "/v1/saved/"+uuid.NewV7().String(), nil, bearer(me.AccessToken)), 404, "listing_not_found")
	expect("save a bad id", call(t, server, "PUT", "/v1/saved/x", nil, bearer(me.AccessToken)), 400, "")
	expect("save signed out", call(t, server, "PUT", path, nil), 401, "unauthenticated")
	expect("list signed out", call(t, server, "GET", "/v1/saved", nil), 401, "unauthenticated")
	expect("list a bad cursor", call(t, server, "GET", "/v1/saved?cursor=x", nil, bearer(me.AccessToken)), 400, "")
	expect("unsave", call(t, server, "DELETE", path, nil, bearer(me.AccessToken)), 204, "")
	expect("unsave a bad id", call(t, server, "DELETE", "/v1/saved/x", nil, bearer(me.AccessToken)), 400, "")
	expect("unsave signed out", call(t, server, "DELETE", path, nil), 401, "unauthenticated")
}

// activeHidden posts a listing as token, hides it and returns its ID.
func activeHidden(t *testing.T, token string) string {
	t.Helper()
	l := activeListing(t, token)
	if res := call(t, server, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(token)); res.Status != http.StatusOK {
		t.Fatalf("hide: %d", res.Status)
	}
	return l.ID
}
