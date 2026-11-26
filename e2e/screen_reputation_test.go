package e2e

import (
	"net/http"
	"testing"
)

type capView struct {
	Used int `json:"used"`
	Max  int `json:"max"`
}

type reputationScreen struct {
	Rating struct {
		Average *float64 `json:"average"`
		Count   int      `json:"count"`
	} `json:"rating"`
	BuyerRating struct {
		Average *float64 `json:"average"`
		Count   int      `json:"count"`
	} `json:"buyer_rating"`
	Sold      int    `json:"sold"`
	JoinedAt  string `json:"joined_at"`
	Followers int    `json:"followers"`
	Reply     *struct {
		Percent int `json:"percent"`
		Count   int `json:"count"`
	} `json:"reply"`
	Figures struct {
		CodOrders int `json:"cod_orders"`
		Failed    int `json:"failed"`
	} `json:"figures_90d"`
	Caps struct {
		Listings  capView `json:"listings"`
		CodOrders capView `json:"cod_orders"`
	} `json:"caps"`
}

// reputation reads the view as the account with token.
func reputation(t *testing.T, token string) reputationScreen {
	t.Helper()
	return expect[reputationScreen](t, call(t, "GET", "/v1/screens/reputation", nil, bearer(token)), http.StatusOK)
}

// UC-TRS-04 A1, A2.
func TestReputationScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)

	fresh := reputation(t, seller.AccessToken)
	if fresh.Rating.Average != nil || fresh.Rating.Count != 0 || fresh.Sold != 0 || fresh.Followers != 0 || fresh.Reply != nil ||
		fresh.JoinedAt == "" || fresh.Figures.CodOrders != 0 || fresh.Figures.Failed != 0 ||
		fresh.Caps.Listings != (capView{0, 20}) || fresh.Caps.CodOrders != (capView{0, 3}) {
		t.Fatalf("a new account: %+v", fresh)
	}

	l := activeListing(t, seller.AccessToken, nil)
	if got := reputation(t, seller.AccessToken); got.Caps.Listings.Used != 1 {
		t.Fatalf("a listing on sale: %+v", got.Caps)
	}
	expectStatus(t, call(t, "POST", "/v1/follows", map[string]any{"account_id": seller.Account.ID}, bearer(buyer.AccessToken)), http.StatusNoContent)
	shipped := shippedOrder(t, seller.AccessToken, buyer.AccessToken, l.ID)
	if got := reputation(t, buyer.AccessToken); got.Figures.CodOrders != 1 || got.Caps.CodOrders.Used != 1 {
		t.Fatalf("the buyer with an order on its way: %+v", got)
	}
	orderMoved(t, buyer.AccessToken, shipped.Order.ID, "confirm", nil)
	expectStatus(t, call(t, "PUT", "/v1/orders/"+shipped.Order.ID+"/review", map[string]any{"stars": 4}, bearer(buyer.AccessToken)), http.StatusOK)
	// The seller's review of the buyer shows both.
	expectStatus(t, call(t, "PUT", "/v1/orders/"+shipped.Order.ID+"/review", map[string]any{"stars": 5}, bearer(seller.AccessToken)), http.StatusOK)

	got := reputation(t, seller.AccessToken)
	if got.Sold != 1 || got.Rating.Count != 1 || got.Rating.Average == nil || *got.Rating.Average != 4 || got.Followers != 1 ||
		got.Caps.Listings.Used != 0 || got.Figures.CodOrders != 0 {
		t.Fatalf("the seller after the sale: %+v", got)
	}
	if got = reputation(t, buyer.AccessToken); got.Figures.CodOrders != 1 || got.Figures.Failed != 0 || got.Caps.CodOrders.Used != 0 ||
		got.Sold != 0 || got.BuyerRating.Count != 1 || got.BuyerRating.Average == nil || *got.BuyerRating.Average != 5 {
		t.Fatalf("the buyer after the sale: %+v", got)
	}

	check(t, "signed out", call(t, "GET", "/v1/screens/reputation", nil), 401, "unauthenticated")
}
