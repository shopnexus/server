package e2e

import (
	"net/http"
	"testing"
	"uuid"
)

type savedListing struct {
	ListingID  string `json:"listing_id"`
	SavedPrice int64  `json:"saved_price"`
	Listing    struct {
		Title        string `json:"title"`
		CategoryCode string `json:"category_code"`
		Condition    string `json:"condition"`
		Commune      string `json:"commune"`
		Status       string `json:"status"`
	} `json:"listing"`
}

func TestSaved(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	me, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	path := "/v1/saved/" + l.ID

	check(t, "save", call(t, "PUT", path, nil, bearer(me.AccessToken)), 204, "")
	check(t, "save again", call(t, "PUT", path, nil, bearer(me.AccessToken)), 204, "")
	res := call(t, "GET", "/v1/saved", nil, bearer(me.AccessToken))
	if got := decode[page[savedListing]](t, res); res.Status != http.StatusOK || len(got.Items) != 1 || got.Items[0].Listing.Title == "" ||
		got.Items[0].Listing.CategoryCode != "vehicles.bicycle" || got.Items[0].Listing.Condition != "used" || got.Items[0].Listing.Commune == "" {
		t.Fatalf("list: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(seller.AccessToken)), http.StatusOK)
	res = call(t, "GET", "/v1/saved", nil, bearer(me.AccessToken))
	if got := decode[page[savedListing]](t, res); got.Items[0].Listing.Status != "hidden" || got.Items[0].Listing.Title != "" ||
		got.Items[0].Listing.CategoryCode != "" || got.Items[0].Listing.Commune != "" {
		t.Fatalf("hidden: %s", res.Body)
	}
	check(t, "save a hidden listing", call(t, "PUT", "/v1/saved/"+activeHidden(t, seller.AccessToken), nil, bearer(me.AccessToken)), 409, "listing_unavailable")
	check(t, "save own", call(t, "PUT", "/v1/saved/"+activeListing(t, seller.AccessToken, nil).ID, nil, bearer(seller.AccessToken)), 409, "own_listing")
	check(t, "save unknown", call(t, "PUT", "/v1/saved/"+uuid.NewV7().String(), nil, bearer(me.AccessToken)), 404, "listing_not_found")
	check(t, "save a bad id", call(t, "PUT", "/v1/saved/x", nil, bearer(me.AccessToken)), 400, "")
	check(t, "save signed out", call(t, "PUT", path, nil), 401, "unauthenticated")
	check(t, "list signed out", call(t, "GET", "/v1/saved", nil), 401, "unauthenticated")
	check(t, "list a bad cursor", call(t, "GET", "/v1/saved?cursor=x", nil, bearer(me.AccessToken)), 400, "")
	check(t, "unsave", call(t, "DELETE", path, nil, bearer(me.AccessToken)), 204, "")
	check(t, "unsave a bad id", call(t, "DELETE", "/v1/saved/x", nil, bearer(me.AccessToken)), 400, "")
	check(t, "unsave signed out", call(t, "DELETE", path, nil), 401, "unauthenticated")
}
