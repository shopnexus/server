package e2e

import (
	"net/http"
	"testing"
	"uuid"
)

// sellerScreen is GET /v1/screens/seller/{id}; a nil pointer is an absent
// key.
type sellerScreen struct {
	Seller struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
		Bio         string `json:"bio"`
		JoinedAt    string `json:"joined_at"`
		Rating      struct {
			Average float64 `json:"average"`
			Count   int     `json:"count"`
		} `json:"rating"`
		Sold      int `json:"sold"`
		OnSale    int `json:"on_sale"`
		Followers int `json:"followers"`
	} `json:"seller"`
	Listings sellerListings `json:"listings"`
	Reviews  page[struct {
		Stars int `json:"stars"`
		Buyer struct {
			AvatarURL string `json:"avatar_url"`
		} `json:"buyer"`
	}] `json:"reviews"`
	Viewer *struct {
		Blocked        bool    `json:"blocked"`
		Following      bool    `json:"following"`
		ConversationID *string `json:"conversation_id"`
	} `json:"viewer"`
}

// sellerListings is a page of a seller's public listings with their
// distances.
type sellerListings struct {
	page[listing]

	Distances []struct {
		ListingID  string `json:"listing_id"`
		DistanceKm int    `json:"distance_km"`
	} `json:"distances"`
}

// The seller view counts the reserved listing on sale and the sold one,
// leaves the hidden one out of both, and shows the avatars of the seller
// and of the reviewing buyer.
func TestSellerScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	token := seller.AccessToken
	expectStatus(t, call(t, "PATCH", "/v1/accounts/me", map[string]any{"display_name": "Linh", "bio": "Đồ cũ còn tốt"}, bearer(token)), http.StatusOK)
	avatar, buyerAvatar := setAvatar(t, token), setAvatar(t, buyer.AccessToken)
	sold := activeListing(t, token, nil).ID
	expectStatus(t, call(t, "POST", "/v1/listings/"+sold+"/mark-sold", nil, bearer(token)), http.StatusOK)
	activeHidden(t, token)
	active := activeListing(t, token, nil).ID
	reserved := activeListing(t, token, nil).ID
	order := shippedOrder(t, token, buyer.AccessToken, reserved)
	expectStatus(t, call(t, "PUT", "/v1/orders/"+order.Order.ID+"/review", map[string]any{"stars": 4}, bearer(buyer.AccessToken)), http.StatusOK)
	path := "/v1/screens/seller/" + seller.Account.ID
	get := func(name, path string, headers ...[2]string) sellerScreen {
		t.Helper()
		res := call(t, "GET", path, nil, headers...)
		if res.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", name, res.Status, res.Body)
		}
		return decode[sellerScreen](t, res)
	}

	got := get("signed out", path)
	s := got.Seller
	if s.ID != seller.Account.ID || s.DisplayName != "Linh" || s.Bio != "Đồ cũ còn tốt" || s.AvatarURL != avatar || s.JoinedAt == "" ||
		s.Rating.Average != 4 || s.Rating.Count != 1 || s.Sold != 1 || s.OnSale != 2 {
		t.Fatalf("seller: %+v", s)
	}
	if ls := got.Listings; len(ls.Items) != 2 || ls.Items[0].ID != reserved || ls.Items[1].ID != active || ls.NextCursor != "" ||
		ls.Distances != nil {
		t.Fatalf("listings: %+v", ls)
	}
	// With a position each listing has its distance, in the order of items;
	// the listings are in commune 00004.
	if ls := get("near", path+"?near_commune=00004").Listings; len(ls.Distances) != 2 ||
		ls.Distances[0].ListingID != reserved || ls.Distances[0].DistanceKm != 0 || ls.Distances[1].ListingID != active {
		t.Fatalf("distances: %+v", ls)
	}
	check(t, "an unknown commune", call(t, "GET", path+"?near_commune=nope", nil), 400, "invalid_position")
	if r := got.Reviews; len(r.Items) != 1 || r.Items[0].Stars != 4 ||
		r.Items[0].Buyer.AvatarURL != buyerAvatar || r.NextCursor != "" {
		t.Fatalf("reviews: %+v", r)
	}
	if got.Viewer != nil {
		t.Fatalf("signed out has a viewer: %+v", got.Viewer)
	}
	if got = get("the seller", path, bearer(token)); got.Viewer != nil {
		t.Fatalf("the seller has a viewer: %+v", got.Viewer)
	}
	if got = get("before chatting", path, bearer(buyer.AccessToken)); got.Viewer == nil || got.Viewer.Blocked || got.Viewer.ConversationID != nil {
		t.Fatalf("before chatting: %+v", got.Viewer)
	}
	if got.Seller.Followers != 0 || got.Viewer.Following {
		t.Fatalf("before following: %d %+v", got.Seller.Followers, got.Viewer)
	}
	expectStatus(t, call(t, "POST", "/v1/follows", map[string]any{"account_id": seller.Account.ID}, bearer(buyer.AccessToken)), http.StatusNoContent)
	if got = get("following", path, bearer(buyer.AccessToken)); got.Seller.Followers != 1 || got.Viewer == nil || !got.Viewer.Following {
		t.Fatalf("following: %d %+v", got.Seller.Followers, got.Viewer)
	}
	if got = get("signed out, following", path); got.Seller.Followers != 1 {
		t.Fatalf("the count is public: %d", got.Seller.Followers)
	}
	conversation := openChat(t, buyer.AccessToken, active, "Còn không bạn?")
	if got = get("after chatting", path, bearer(buyer.AccessToken)); got.Viewer == nil || got.Viewer.Blocked ||
		got.Viewer.ConversationID == nil || *got.Viewer.ConversationID != conversation.ID {
		t.Fatalf("after chatting: %+v", got.Viewer)
	}

	// A block shows to both of the two.
	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": buyer.Account.ID}, bearer(token)), http.StatusNoContent)
	if got = get("blocked by the seller", path, bearer(buyer.AccessToken)); got.Viewer == nil || !got.Viewer.Blocked {
		t.Fatalf("blocked by the seller: %+v", got.Viewer)
	}
	if got.Viewer.Following || got.Seller.Followers != 0 {
		t.Fatalf("the block left the follow: %d %+v", got.Seller.Followers, got.Viewer)
	}
	if got = get("the blocker", "/v1/screens/seller/"+buyer.Account.ID, bearer(token)); got.Viewer == nil || !got.Viewer.Blocked {
		t.Fatalf("the blocker: %+v", got.Viewer)
	}

	// Load more goes through the list endpoints.
	listings := "/v1/sellers/" + seller.Account.ID + "/listings"
	res := call(t, "GET", listings+"?limit=1", nil)
	first := decode[sellerListings](t, res)
	if res.Status != http.StatusOK || len(first.Items) != 1 || first.Items[0].ID != reserved || first.NextCursor == "" || first.Distances != nil {
		t.Fatalf("first page: %d %s", res.Status, res.Body)
	}
	res = call(t, "GET", listings+"?limit=1&near_commune=00004&cursor="+first.NextCursor, nil, bearer(buyer.AccessToken))
	if rest := decode[sellerListings](t, res); res.Status != http.StatusOK || len(rest.Items) != 1 || rest.Items[0].ID != active || rest.NextCursor != "" ||
		len(rest.Distances) != 1 || rest.Distances[0].ListingID != active || rest.Distances[0].DistanceKm != 0 {
		t.Fatalf("second page: %d %s", res.Status, res.Body)
	}
	check(t, "listings near an unknown commune", call(t, "GET", listings+"?near_commune=nope", nil), 400, "invalid_position")
	check(t, "listings of a bad id", call(t, "GET", "/v1/sellers/x/listings", nil), 400, "")
	check(t, "listings with a bad cursor", call(t, "GET", listings+"?cursor=x", nil), 400, "")
	check(t, "listings of an unknown seller", call(t, "GET", "/v1/sellers/"+uuid.NewV7().String()+"/listings", nil), 404, "account_not_found")

	check(t, "an unknown seller", call(t, "GET", "/v1/screens/seller/"+uuid.NewV7().String(), nil), 404, "account_not_found")
	check(t, "a bad id", call(t, "GET", "/v1/screens/seller/x", nil), 400, "")
	check(t, "a bad token", call(t, "GET", path, nil, bearer("x")), 401, "")
}
