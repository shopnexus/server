package e2e

import (
	"net/http"
	"testing"
	"time"
)

// awayView reads away_until wherever a view carries it; nil is absent.
type awayView struct {
	AwayUntil *time.Time `json:"away_until"`
	Seller    struct {
		AwayUntil *time.Time `json:"away_until"`
	} `json:"seller"`
	Account struct {
		AwayUntil *time.Time `json:"away_until"`
	} `json:"account"`
	Other struct {
		AwayUntil *time.Time `json:"away_until"`
	} `json:"other"`
}

// Away mode refuses a buyer's offer, accept and COD request with
// seller_away and shows on the seller's own and public views until cleared.
func TestAwayMode(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	setAway := func(until any) Response {
		t.Helper()
		return call(t, "PATCH", "/v1/accounts/me", map[string]any{"away_until": until}, bearer(seller.AccessToken))
	}
	read := func(token, path string) awayView {
		t.Helper()
		return expect[awayView](t, call(t, "GET", path, nil, bearer(token)), http.StatusOK)
	}

	check(t, "in the past", setAway(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)), 422, "invalid_account")
	check(t, "40 days ahead", setAway(time.Now().Add(40*24*time.Hour).UTC().Format(time.RFC3339)), 422, "invalid_account")
	until := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	if v := expect[awayView](t, setAway(until.Format(time.RFC3339)), http.StatusOK); v.AwayUntil == nil || !v.AwayUntil.Equal(until) {
		t.Fatalf("set: %+v", v)
	}
	if v := read(seller.AccessToken, "/v1/accounts/me"); v.AwayUntil == nil || !v.AwayUntil.Equal(until) {
		t.Errorf("me: %+v", v)
	}
	if v := read(seller.AccessToken, "/v1/screens/profile-tasks"); v.Account.AwayUntil == nil {
		t.Errorf("profile tasks: %+v", v)
	}
	if v := read(buyer.AccessToken, "/v1/screens/listing/"+l.ID); v.Seller.AwayUntil == nil || !v.Seller.AwayUntil.Equal(until) {
		t.Errorf("listing screen: %+v", v)
	}
	if v := read(buyer.AccessToken, "/v1/screens/seller/"+seller.Account.ID); v.Seller.AwayUntil == nil {
		t.Errorf("seller screen: %+v", v)
	}

	// Chat and opening the deal go on; the moves that bind the seller wait.
	conv := openChat(t, buyer.AccessToken, l.ID, "Bạn về chưa?")
	if v := read(buyer.AccessToken, "/v1/screens/chat/"+conv.ID); v.Other.AwayUntil == nil || !v.Other.AwayUntil.Equal(until) {
		t.Errorf("chat screen: %+v", v)
	}
	inbox := expect[page[awayView]](t, call(t, "GET", "/v1/screens/inbox", nil, bearer(buyer.AccessToken)), http.StatusOK)
	if len(inbox.Items) != 1 || inbox.Items[0].Other.AwayUntil == nil {
		t.Errorf("inbox: %+v", inbox)
	}
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	check(t, "offer", dealCall(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000}), 409, "seller_away")
	check(t, "buy at the listed price", dealCall(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000}), 409, "seller_away")

	if v := expect[awayView](t, setAway(nil), http.StatusOK); v.AwayUntil != nil {
		t.Fatalf("clear: %+v", v)
	}
	if v := read(buyer.AccessToken, "/v1/screens/listing/"+l.ID); v.Seller.AwayUntil != nil {
		t.Errorf("listing screen after clearing: %+v", v)
	}
	if v := read(buyer.AccessToken, "/v1/screens/chat/"+conv.ID); v.Other.AwayUntil != nil {
		t.Errorf("chat screen after clearing: %+v", v)
	}
	moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000})
	expectStatus(t, setAway(until.Format(time.RFC3339)), http.StatusOK)
	check(t, "cod", call(t, "POST", "/v1/deals/"+d.ID+"/cod", codBody(deliveryAddress(t, buyer.AccessToken)),
		bearer(buyer.AccessToken), newKey()), 409, "seller_away")
}
