package e2e

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"uuid"
)

// listingScreen is GET /v1/screens/listing/{id}; a nil pointer is an
// absent key.
type listingScreen struct {
	Listing    listing `json:"listing"`
	DistanceKm *int    `json:"distance_km"`
	Seller     struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		JoinedAt    string `json:"joined_at"`
		Rating      struct {
			Average float64 `json:"average"`
			Count   int     `json:"count"`
		} `json:"rating"`
		Sold int `json:"sold"`
	} `json:"seller"`
	Viewer *struct {
		Saved bool `json:"saved"`
		Deal  *struct {
			ID       string `json:"id"`
			Role     string `json:"role"`
			YourTurn bool   `json:"your_turn"`
		} `json:"deal"`
		ConversationID *string `json:"conversation_id"`
	} `json:"viewer"`
}

func TestListingScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	reader, _ := newAccount(t)
	expectStatus(t, call(t, "PATCH", "/v1/accounts/me", map[string]any{"display_name": "Linh"}, bearer(seller.AccessToken)), http.StatusOK)
	avatar := setAvatar(t, seller.AccessToken)
	sold := activeListing(t, seller.AccessToken, nil).ID
	expectStatus(t, call(t, "POST", "/v1/listings/"+sold+"/mark-sold", nil, bearer(seller.AccessToken)), http.StatusOK)
	reviewer, _ := newAccount(t)
	reviewed := shippedOrder(t, seller.AccessToken, reviewer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	expectStatus(t, call(t, "PUT", "/v1/orders/"+reviewed.Order.ID+"/review", map[string]any{"stars": 4}, bearer(reviewer.AccessToken)), http.StatusOK)
	lo, _ := band()
	l := activeListing(t, seller.AccessToken, map[string]any{"price": lo})
	path := "/v1/screens/listing/" + l.ID
	get := func(name string, headers ...[2]string) listingScreen {
		t.Helper()
		res := call(t, "GET", path, nil, headers...)
		if res.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", name, res.Status, res.Body)
		}
		return decode[listingScreen](t, res)
	}

	got := get("signed out")
	if got.Listing.ID != l.ID || got.Seller.ID != seller.Account.ID || got.Seller.DisplayName != "Linh" ||
		got.Seller.AvatarURL != avatar || got.Seller.JoinedAt == "" || got.Seller.Sold != 1 ||
		got.Seller.Rating.Average != 4 || got.Seller.Rating.Count != 1 {
		t.Fatalf("signed out: %+v", got)
	}
	if got.Viewer != nil || got.DistanceKm != nil {
		t.Fatalf("signed out has a viewer or a distance: %+v", got)
	}
	// With a position the view has the listing's distance, as search
	// measures it; the listing is in commune 00004.
	res := call(t, "GET", path+"?near_commune=00004", nil)
	if near := decode[listingScreen](t, res); res.Status != http.StatusOK || near.DistanceKm == nil || *near.DistanceKm != 0 {
		t.Fatalf("near its commune: %d %s", res.Status, res.Body)
	}
	price := strconv.FormatInt(lo, 10)
	_, found := search(t, url.Values{"near_commune": {"00025"}, "price_min": {price}, "price_max": {price}})
	res = call(t, "GET", path+"?near_commune=00025", nil)
	far := decode[listingScreen](t, res)
	if res.Status != http.StatusOK || far.DistanceKm == nil || len(found.Items) != 1 || found.Items[0].ID != l.ID ||
		found.Items[0].DistanceKm == nil || *found.Items[0].DistanceKm != *far.DistanceKm {
		t.Fatalf("the view and search disagree: %s %+v", res.Body, found.Items)
	}
	check(t, "an unknown commune", call(t, "GET", path+"?near_commune=nope", nil), 400, "invalid_position")
	check(t, "a latitude alone", call(t, "GET", path+"?near_lat=21.03", nil), 400, "invalid_position")
	if got = get("seller", bearer(seller.AccessToken)); got.Viewer != nil {
		t.Fatalf("seller: %+v", got.Viewer)
	}
	if got = get("before", bearer(buyer.AccessToken)); got.Viewer == nil || got.Viewer.Saved || got.Viewer.Deal != nil || got.Viewer.ConversationID != nil {
		t.Fatalf("nothing yet: %+v", got.Viewer)
	}

	expectStatus(t, call(t, "PUT", "/v1/saved/"+l.ID, nil, bearer(buyer.AccessToken)), http.StatusNoContent)
	deal := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	moved(t, buyer.AccessToken, deal.ID, "offer", map[string]any{"amount": lo - 100_000})
	conversation := openChat(t, buyer.AccessToken, l.ID, "Còn không bạn?")
	got = get("buyer", bearer(buyer.AccessToken))
	if v := got.Viewer; v == nil || !v.Saved || v.Deal == nil || v.Deal.ID != deal.ID || v.Deal.Role != "buyer" || v.Deal.YourTurn ||
		v.ConversationID == nil || *v.ConversationID != conversation.ID {
		t.Fatalf("buyer: %+v", got.Viewer)
	}
	if got = get("another reader", bearer(reader.AccessToken)); got.Viewer == nil || got.Viewer.Saved || got.Viewer.Deal != nil || got.Viewer.ConversationID != nil {
		t.Fatalf("another reader: %+v", got.Viewer)
	}

	// The view shows only a public listing, to its seller too.
	expectStatus(t, call(t, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(seller.AccessToken)), http.StatusOK)
	check(t, "hidden, to the seller", call(t, "GET", path, nil, bearer(seller.AccessToken)), 404, "listing_not_found")
	check(t, "hidden, signed out", call(t, "GET", path, nil), 404, "listing_not_found")
	check(t, "sold", call(t, "GET", "/v1/screens/listing/"+sold, nil), 404, "listing_not_found")
	check(t, "unknown", call(t, "GET", "/v1/screens/listing/"+uuid.NewV7().String(), nil), 404, "listing_not_found")
	check(t, "a bad id", call(t, "GET", "/v1/screens/listing/x", nil), 400, "")
	check(t, "a bad token", call(t, "GET", path, nil, bearer("x")), 401, "")
}
