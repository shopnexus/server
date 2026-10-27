package e2e

import (
	"net/http"
	"testing"
	"uuid"
)

type pageOfListings struct {
	Items      []listing `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

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
		Sold   int `json:"sold"`
		OnSale int `json:"on_sale"`
	} `json:"seller"`
	Listings pageOfListings `json:"listings"`
	Reviews  struct {
		Average float64 `json:"average"`
		Count   int     `json:"count"`
		Items   []struct {
			Stars int `json:"stars"`
			Buyer struct {
				AvatarURL string `json:"avatar_url"`
			} `json:"buyer"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	} `json:"reviews"`
	Viewer *struct {
		Blocked        bool    `json:"blocked"`
		ConversationID *string `json:"conversation_id"`
	} `json:"viewer"`
}

// The seller view counts the reserved listing on sale and the sold one,
// leaves the hidden one out of both, and shows the avatars of the seller
// and of the reviewing buyer.
func TestSellerScreen(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	token := seller.AccessToken
	if res := call(t, server, "PATCH", "/v1/accounts/me", map[string]any{"display_name": "Linh", "bio": "Đồ cũ còn tốt"}, bearer(token)); res.Status != http.StatusOK {
		t.Fatalf("profile: %d %s", res.Status, res.Body)
	}
	avatar, buyerAvatar := setAvatar(t, token), setAvatar(t, buyer.AccessToken)
	sold := activeListing(t, token).ID
	if res := call(t, server, "POST", "/v1/listings/"+sold+"/mark-sold", nil, bearer(token)); res.Status != http.StatusOK {
		t.Fatalf("mark sold: %d", res.Status)
	}
	hidden := activeListing(t, token).ID
	if res := call(t, server, "POST", "/v1/listings/"+hidden+"/hide", nil, bearer(token)); res.Status != http.StatusOK {
		t.Fatalf("hide: %d", res.Status)
	}
	active := activeListing(t, token).ID
	reserved := activeListing(t, token).ID
	order := requestedOrder(t, buyer.AccessToken, reserved)
	if res := orderCall(t, token, order.ID, "ship", waybillBody); res.Status != http.StatusOK {
		t.Fatalf("ship: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "PUT", "/v1/orders/"+order.Order.ID+"/review", map[string]any{"stars": 4}, bearer(buyer.AccessToken)); res.Status != http.StatusOK {
		t.Fatalf("review: %d %s", res.Status, res.Body)
	}
	path := "/v1/screens/seller/" + seller.Account.ID
	get := func(name, path string, headers ...[2]string) sellerScreen {
		t.Helper()
		res := call(t, server, "GET", path, nil, headers...)
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
	if ls := got.Listings; len(ls.Items) != 2 || ls.Items[0].ID != reserved || ls.Items[1].ID != active || ls.NextCursor != "" {
		t.Fatalf("listings: %+v", ls)
	}
	if r := got.Reviews; r.Average != 4 || r.Count != 1 || len(r.Items) != 1 || r.Items[0].Stars != 4 ||
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
	conversation := openChat(t, buyer.AccessToken, active, "Còn không bạn?")
	if got = get("after chatting", path, bearer(buyer.AccessToken)); got.Viewer == nil || got.Viewer.Blocked ||
		got.Viewer.ConversationID == nil || *got.Viewer.ConversationID != conversation.ID {
		t.Fatalf("after chatting: %+v", got.Viewer)
	}

	// A block shows to both of the two.
	if res := call(t, server, "POST", "/v1/blocks", map[string]any{"account_id": buyer.Account.ID}, bearer(token)); res.Status != http.StatusNoContent {
		t.Fatalf("block: %d %s", res.Status, res.Body)
	}
	if got = get("blocked by the seller", path, bearer(buyer.AccessToken)); got.Viewer == nil || !got.Viewer.Blocked {
		t.Fatalf("blocked by the seller: %+v", got.Viewer)
	}
	if got = get("the blocker", "/v1/screens/seller/"+buyer.Account.ID, bearer(token)); got.Viewer == nil || !got.Viewer.Blocked {
		t.Fatalf("the blocker: %+v", got.Viewer)
	}

	// Load more goes through the list endpoints.
	listings := "/v1/sellers/" + seller.Account.ID + "/listings"
	res := call(t, server, "GET", listings+"?limit=1", nil)
	first := decode[pageOfListings](t, res)
	if res.Status != http.StatusOK || len(first.Items) != 1 || first.Items[0].ID != reserved || first.NextCursor == "" {
		t.Fatalf("first page: %d %s", res.Status, res.Body)
	}
	res = call(t, server, "GET", listings+"?limit=1&cursor="+first.NextCursor, nil, bearer(buyer.AccessToken))
	if rest := decode[pageOfListings](t, res); res.Status != http.StatusOK || len(rest.Items) != 1 || rest.Items[0].ID != active || rest.NextCursor != "" {
		t.Fatalf("second page: %d %s", res.Status, res.Body)
	}
	expect("listings of a bad id", call(t, server, "GET", "/v1/sellers/x/listings", nil), 400, "")
	expect("listings with a bad cursor", call(t, server, "GET", listings+"?cursor=x", nil), 400, "")
	expect("listings of an unknown seller", call(t, server, "GET", "/v1/sellers/"+uuid.NewV7().String()+"/listings", nil), 404, "account_not_found")

	expect("an unknown seller", call(t, server, "GET", "/v1/screens/seller/"+uuid.NewV7().String(), nil), 404, "account_not_found")
	expect("a bad id", call(t, server, "GET", "/v1/screens/seller/x", nil), 400, "")
	expect("a bad token", call(t, server, "GET", path, nil, bearer("x")), 401, "")
}
