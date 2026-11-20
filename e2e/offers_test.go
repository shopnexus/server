package e2e

import (
	"net/http"
	"testing"
)

// offersScreen is the OffersScreen schema.
type offersScreen struct {
	Items []struct {
		Deal  dealView `json:"deal"`
		Buyer struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"buyer"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func TestSellerReadsOpenOffers(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})

	offers := expect[offersScreen](t, call(t, "GET", "/v1/screens/offers", nil, bearer(seller.AccessToken)), http.StatusOK)
	if len(offers.Items) != 1 || offers.Items[0].Deal.ID != d.ID || offers.Items[0].Buyer.ID != buyer.Account.ID || offers.NextCursor != "" {
		t.Fatalf("offers: %+v", offers)
	}
	if r := offers.Items[0].Deal.OfferRange; r == nil || r.Min != 1_001_000 {
		t.Fatalf("counter range: %+v", r)
	}
	if got := expect[map[string]int](t, call(t, "GET", "/v1/me/unread", nil, bearer(seller.AccessToken)), http.StatusOK); got["offers"] != 1 {
		t.Fatalf("seller's unread: %v", got)
	}
	if got := expect[map[string]int](t, call(t, "GET", "/v1/me/unread", nil, bearer(buyer.AccessToken)), http.StatusOK); got["offers"] != 0 {
		t.Fatalf("buyer's unread: %v", got)
	}

	moved(t, seller.AccessToken, d.ID, "decline", nil)
	if offers = expect[offersScreen](t, call(t, "GET", "/v1/screens/offers?limit=5", nil, bearer(seller.AccessToken)), http.StatusOK); len(offers.Items) != 0 {
		t.Fatalf("offers after the decline: %+v", offers)
	}
	check(t, "offers signed out", call(t, "GET", "/v1/screens/offers", nil), 401, "unauthenticated")
	check(t, "offers with a bad cursor", call(t, "GET", "/v1/screens/offers?cursor=x", nil, bearer(seller.AccessToken)), 400, "")
}
