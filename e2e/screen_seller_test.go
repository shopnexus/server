package e2e

import (
	"net/http"
	"slices"
	"strings"
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
		BuyerRating struct {
			Count int `json:"count"`
		} `json:"buyer_rating"`
		Sold      int `json:"sold"`
		OnSale    int `json:"on_sale"`
		Followers int `json:"followers"`
	} `json:"seller"`
	Listings page[searchCard] `json:"listings"`
	Reviews  page[struct {
		Stars    int `json:"stars"`
		Reviewer struct {
			AvatarURL string `json:"avatar_url"`
		} `json:"reviewer"`
	}] `json:"reviews"`
	BuyerReviews page[struct {
		Stars int `json:"stars"`
	}] `json:"buyer_reviews"`
	Viewer *struct {
		Blocked        bool    `json:"blocked"`
		Following      bool    `json:"following"`
		ConversationID *string `json:"conversation_id"`
	} `json:"viewer"`
}

// cards checks a page of the seller's cards: their IDs in order, which of
// them are saved, and that each has a distance when near is set and none
// otherwise.
func cards(t *testing.T, name string, got page[searchCard], seller string, near bool, want []string, saved ...string) {
	t.Helper()
	if len(got.Items) != len(want) {
		t.Fatalf("%s: %+v, want %v", name, got, want)
	}
	for i := range got.Items {
		c := &got.Items[i]
		if c.ID != want[i] || c.SellerID != seller || c.CoverThumbURL == "" || c.Commune == "" || c.CommuneCode == "" ||
			c.Saved != slices.Contains(saved, c.ID) || (c.DistanceKm != nil) != near || (near && *c.DistanceKm != 0) {
			t.Fatalf("%s: card %d is %+v", name, i, c)
		}
	}
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
	shippedOrder(t, token, buyer.AccessToken, reserved)
	order := completedOrder(t, token, buyer.AccessToken, activeListing(t, token, nil).ID)
	expectStatus(t, call(t, "PUT", "/v1/saved/"+active, nil, bearer(buyer.AccessToken)), http.StatusNoContent)
	expectStatus(t, call(t, "PUT", "/v1/orders/"+order.Order.ID+"/review", map[string]any{"stars": 4}, bearer(buyer.AccessToken)), http.StatusOK)
	revealReviews(t, order.Order.ID)
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
		s.Rating.Average != 4 || s.Rating.Count != 1 || s.BuyerRating.Count != 0 || s.Sold != 2 || s.OnSale != 2 {
		t.Fatalf("seller: %+v", s)
	}
	if got.Listings.NextCursor != "" {
		t.Fatalf("listings: %+v", got.Listings)
	}
	cards(t, "listings signed out", got.Listings, seller.Account.ID, false, []string{reserved, active})
	// With a position each card has its distance, as the listings are in
	// commune 00004, and the buyer's cards mark the active one saved.
	cards(t, "listings near", get("near", path+"?near_commune=00004").Listings, seller.Account.ID, true, []string{reserved, active})
	cards(t, "listings of the saver", get("saver", path+"?near_commune=00004", bearer(buyer.AccessToken)).Listings,
		seller.Account.ID, true, []string{reserved, active}, active)
	check(t, "an unknown commune", call(t, "GET", path+"?near_commune=nope", nil), 400, "invalid_position")
	if r := got.Reviews; len(r.Items) != 1 || r.Items[0].Stars != 4 ||
		r.Items[0].Reviewer.AvatarURL != buyerAvatar || r.NextCursor != "" || len(got.BuyerReviews.Items) != 0 {
		t.Fatalf("reviews: %+v", r)
	}
	if got.Viewer != nil {
		t.Fatalf("signed out has a viewer: %+v", got.Viewer)
	}
	if got = get("the seller", path, bearer(token)); got.Viewer != nil {
		t.Fatalf("the seller has a viewer: %+v", got.Viewer)
	}
	// The buyer's deal opened the pair's conversation before any message.
	if got = get("before chatting", path, bearer(buyer.AccessToken)); got.Viewer == nil || got.Viewer.Blocked ||
		got.Viewer.ConversationID == nil || *got.Viewer.ConversationID != order.ConversationID {
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
	first := decode[page[searchCard]](t, res)
	if res.Status != http.StatusOK || first.NextCursor == "" {
		t.Fatalf("first page: %d %s", res.Status, res.Body)
	}
	cards(t, "first page", first, seller.Account.ID, false, []string{reserved})
	res = call(t, "GET", listings+"?limit=1&near_commune=00004&cursor="+first.NextCursor, nil, bearer(buyer.AccessToken))
	rest := decode[page[searchCard]](t, res)
	if res.Status != http.StatusOK || rest.NextCursor != "" {
		t.Fatalf("second page: %d %s", res.Status, res.Body)
	}
	cards(t, "second page of the saver", rest, seller.Account.ID, true, []string{active}, active)
	res = call(t, "GET", listings+"?near_commune=00004&cursor="+first.NextCursor, nil)
	cards(t, "second page signed out", decode[page[searchCard]](t, res), seller.Account.ID, true, []string{active})
	check(t, "listings near an unknown commune", call(t, "GET", listings+"?near_commune=nope", nil), 400, "invalid_position")
	check(t, "listings of an unknown seller near an unknown commune",
		call(t, "GET", "/v1/sellers/"+uuid.NewV7().String()+"/listings?near_commune=nope", nil), 404, "account_not_found")
	res = call(t, "GET", "/v1/sellers/"+buyer.Account.ID+"/listings?near_commune=00004", nil)
	if res.Status != http.StatusOK || !strings.Contains(string(res.Body), `"items":[]`) {
		t.Fatalf("an empty page with a position: %d %s", res.Status, res.Body)
	}
	check(t, "listings with a bad token", call(t, "GET", listings, nil, bearer("x")), 401, "")

	// A listing the reader hid leaves their cards on the view and in load
	// more, and only theirs.
	hider, _ := newAccount(t)
	expectStatus(t, call(t, "PUT", "/v1/hidden/"+reserved, nil, bearer(hider.AccessToken)), http.StatusNoContent)
	cards(t, "the view of the hider", get("hider", path, bearer(hider.AccessToken)).Listings, seller.Account.ID, false, []string{active})
	res = call(t, "GET", listings, nil, bearer(hider.AccessToken))
	cards(t, "load more of the hider", decode[page[searchCard]](t, res), seller.Account.ID, false, []string{active})
	cards(t, "the view signed out", get("signed out", path).Listings, seller.Account.ID, false, []string{reserved, active})
	check(t, "listings of a bad id", call(t, "GET", "/v1/sellers/x/listings", nil), 400, "")
	check(t, "listings with a bad cursor", call(t, "GET", listings+"?cursor=x", nil), 400, "")
	check(t, "listings of an unknown seller", call(t, "GET", "/v1/sellers/"+uuid.NewV7().String()+"/listings", nil), 404, "account_not_found")

	check(t, "an unknown seller", call(t, "GET", "/v1/screens/seller/"+uuid.NewV7().String(), nil), 404, "account_not_found")
	check(t, "a bad id", call(t, "GET", "/v1/screens/seller/x", nil), 400, "")
	check(t, "a bad token", call(t, "GET", path, nil, bearer("x")), 401, "")
}
