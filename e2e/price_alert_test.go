package e2e

import (
	"net/http"
	"testing"
	"uuid"
)

func TestPriceAlert(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	me, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	path := "/v1/saved/" + l.ID + "/alert"
	check(t, "set", call(t, "PUT", path, map[string]any{"below": l.Price}, bearer(me.AccessToken)), 204, "")
	res := call(t, "GET", "/v1/saved", nil, bearer(me.AccessToken))
	saved := expect[page[struct {
		ListingID  string `json:"listing_id"`
		AlertBelow *int64 `json:"alert_below"`
	}]](t, res, http.StatusOK)
	if len(saved.Items) != 1 || saved.Items[0].AlertBelow == nil || *saved.Items[0].AlertBelow != l.Price {
		t.Fatalf("saved with its level: %s", res.Body)
	}
	body := draftWith(l.Photos[0].UploadID)
	body["price"] = l.Price - 100_000
	expectStatus(t, call(t, "PUT", "/v1/listings/"+l.ID, body, bearer(seller.AccessToken), newKey()), http.StatusOK)
	got := notices(t, me.AccessToken, 1)
	if n := got.Items[0]; n.Type != "price_dropped" || n.Target.Kind != "listing" || n.Target.ID != l.ID {
		t.Fatalf("%+v", n)
	}

	check(t, "set a bad body", call(t, "PUT", path, map[string]any{"below": 5}, bearer(me.AccessToken)), 400, "")
	check(t, "set on own", call(t, "PUT", path, map[string]any{"below": 100_000}, bearer(seller.AccessToken)), 409, "own_listing")
	check(t, "set on unknown", call(t, "PUT", "/v1/saved/"+uuid.NewV7().String()+"/alert", map[string]any{"below": 100_000}, bearer(me.AccessToken)), 404, "listing_not_found")
	check(t, "set signed out", call(t, "PUT", path, map[string]any{"below": 100_000}), 401, "unauthenticated")
	check(t, "clear", call(t, "DELETE", path, nil, bearer(me.AccessToken)), 204, "")
	check(t, "clear a bad id", call(t, "DELETE", "/v1/saved/x/alert", nil, bearer(me.AccessToken)), 400, "")
	check(t, "clear signed out", call(t, "DELETE", path, nil), 401, "unauthenticated")
}
