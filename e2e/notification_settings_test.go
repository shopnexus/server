package e2e

import (
	"net/http"
	"testing"
)

type quietHours struct {
	On   bool   `json:"on"`
	From string `json:"from"`
	To   string `json:"to"`
}

type notificationSettings struct {
	Messages    bool       `json:"messages"`
	Offers      bool       `json:"offers"`
	Orders      bool       `json:"orders"`
	PriceCuts   bool       `json:"price_cuts"`
	Followed    bool       `json:"followed"`
	Reviews     bool       `json:"reviews"`
	OrderEmails bool       `json:"order_emails"`
	Quiet       quietHours `json:"quiet"`
}

// defaultSettings is the body of the defaults, for a test to change.
func defaultSettings() map[string]any {
	return map[string]any{
		"messages": true, "offers": true, "orders": true, "price_cuts": true, "followed": true, "reviews": true,
		"order_emails": true, "quiet": map[string]any{"on": true, "from": "22:00", "to": "07:00"},
	}
}

func TestNotificationSettings(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	const path = "/v1/me/notification-settings"
	got := expect[notificationSettings](t, call(t, "GET", path, nil, bearer(me.AccessToken)), http.StatusOK)
	if want := (notificationSettings{
		Messages: true, Offers: true, Orders: true, PriceCuts: true, Followed: true, Reviews: true, OrderEmails: true,
		Quiet: quietHours{On: true, From: "22:00", To: "07:00"},
	}); got != want {
		t.Fatalf("defaults: %+v", got)
	}

	body := defaultSettings()
	body["offers"], body["followed"], body["order_emails"] = false, false, false
	body["quiet"] = map[string]any{"on": true, "from": "12:00", "to": "13:30"}
	put := expect[notificationSettings](t, call(t, "PUT", path, body, bearer(me.AccessToken)), http.StatusOK)
	got = expect[notificationSettings](t, call(t, "GET", path, nil, bearer(me.AccessToken)), http.StatusOK)
	if got != put || got.Offers || got.Followed || got.OrderEmails || !got.Messages || got.Quiet != (quietHours{On: true, From: "12:00", To: "13:30"}) {
		t.Fatalf("put %+v, then got %+v", put, got)
	}

	bad := defaultSettings()
	bad["quiet"] = map[string]any{"on": true, "from": "24:00", "to": "07:00"}
	check(t, "a bad time", call(t, "PUT", path, bad, bearer(me.AccessToken)), 400, "invalid_request")
	bad["quiet"] = map[string]any{"on": false, "from": "07:00", "to": "07:00"}
	check(t, "from == to", call(t, "PUT", path, bad, bearer(me.AccessToken)), 400, "invalid_quiet_hours")
	partial := defaultSettings()
	delete(partial, "reviews")
	check(t, "a field missing", call(t, "PUT", path, partial, bearer(me.AccessToken)), 400, "invalid_request")
	if after := expect[notificationSettings](t, call(t, "GET", path, nil, bearer(me.AccessToken)), http.StatusOK); after != got {
		t.Fatalf("a refused put changed %+v", after)
	}

	check(t, "read signed out", call(t, "GET", path, nil), 401, "unauthenticated")
	check(t, "put signed out", call(t, "PUT", path, defaultSettings()), 401, "unauthenticated")
}

// With price_cuts off a new low of a save without a level of its own still
// lists its notice, and Đã lưu still shows the saved price.
func TestPriceCutsOffStillLists(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	me, _ := newAccount(t)
	body := defaultSettings()
	body["price_cuts"] = false
	expectStatus(t, call(t, "PUT", "/v1/me/notification-settings", body, bearer(me.AccessToken)), http.StatusOK)
	l := activeListing(t, seller.AccessToken, nil)
	expectStatus(t, call(t, "PUT", "/v1/saved/"+l.ID, nil, bearer(me.AccessToken)), http.StatusNoContent)
	draft := draftWith(l.Photos[0].UploadID)
	draft["price"] = l.Price - 100_000
	expectStatus(t, call(t, "PUT", "/v1/listings/"+l.ID, draft, bearer(seller.AccessToken), newKey()), http.StatusOK)
	if n := notices(t, me.AccessToken, 1).Items[0]; n.Type != "price_dropped" || n.Target.ID != l.ID {
		t.Fatalf("%+v", n)
	}
	saved := expect[page[struct {
		SavedPrice int64 `json:"saved_price"`
	}]](t, call(t, "GET", "/v1/saved", nil, bearer(me.AccessToken)), http.StatusOK)
	if len(saved.Items) != 1 || saved.Items[0].SavedPrice != l.Price {
		t.Fatalf("Đã lưu: %+v", saved)
	}
}
