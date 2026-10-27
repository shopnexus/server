package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"
)

type notificationList struct {
	Items []struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Text   string `json:"text"`
		Unread bool   `json:"unread"`
		Target struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"target"`
	} `json:"items"`
	Unread int `json:"unread"`
}

// notices polls token's list until it holds n notices.
func notices(t *testing.T, token string, n int) notificationList {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		res := call(t, server, "GET", "/v1/notifications", nil, bearer(token))
		if res.Status != http.StatusOK {
			t.Fatalf("list: %d %s", res.Status, res.Body)
		}
		if l := decode[notificationList](t, res); len(l.Items) >= n {
			return l
		}
		if time.Now().After(deadline) {
			t.Fatalf("still fewer than %d notices: %s", n, res.Body)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func putToken(t *testing.T, token, push string) {
	t.Helper()
	if res := call(t, server, "PUT", "/v1/me/push-token", map[string]any{"token": push}, bearer(token)); res.Status != http.StatusNoContent {
		t.Fatalf("put token: %d %s", res.Status, res.Body)
	}
}

func TestNotifications(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	sellerPhone, buyerPhone := "seller-"+uuid.NewV7().String(), "buyer-"+uuid.NewV7().String()
	putToken(t, seller.AccessToken, sellerPhone)
	putToken(t, buyer.AccessToken, buyerPhone)
	l := activeListing(t, seller.AccessToken)
	if live := notices(t, seller.AccessToken, 1); live.Items[0].Type != "listing_live" || live.Items[0].Target.ID != l.ID {
		t.Fatalf("the listing going live: %+v", live)
	}
	phones.wait(t, sellerPhone, 1)
	if res := call(t, server, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("read all: %d %s", res.Status, res.Body)
	}
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)

	if res := dealCall(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000}); res.Status != http.StatusOK {
		t.Fatalf("offer: %d %s", res.Status, res.Body)
	}
	got := notices(t, seller.AccessToken, 2)
	n := got.Items[0]
	if n.Type != "offer" || !n.Unread || got.Unread != 1 || n.Target.Kind != "deal" || n.Target.ID != d.ID ||
		!strings.Contains(n.Text, "1 tr") {
		t.Fatalf("the seller's notice: %+v", got)
	}
	if m := phones.wait(t, sellerPhone, 2); m.Data["notification_id"] != n.ID || m.Tag != "deal:"+d.ID {
		t.Fatalf("the seller's push: %+v", m)
	}

	if res := dealCall(t, buyer.AccessToken, d.ID, "withdraw", nil); res.Status != http.StatusOK {
		t.Fatalf("withdraw: %d %s", res.Status, res.Body)
	}
	if res := dealCall(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_100_000}); res.Status != http.StatusOK {
		t.Fatalf("second offer: %d %s", res.Status, res.Body)
	}
	phones.wait(t, sellerPhone, 3)
	got = notices(t, seller.AccessToken, 2)
	if len(got.Items) != 2 || got.Unread != 1 || !strings.Contains(got.Items[0].Text, "1,1 tr") {
		t.Fatalf("the second offer replaces the unread notice: %+v", got)
	}

	path := "/v1/notifications/" + got.Items[0].ID + "/read"
	if res := call(t, server, "POST", path, nil, bearer(stranger.AccessToken)); res.Status != http.StatusNotFound ||
		errorOf(t, res).Code != "notification_not_found" {
		t.Fatalf("a stranger reads it: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "POST", path, nil, bearer(seller.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("read: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("read all: %d %s", res.Status, res.Body)
	}
	if got = notices(t, seller.AccessToken, 2); got.Unread != 0 || got.Items[0].Unread {
		t.Fatalf("after reading: %+v", got)
	}

	c := openChat(t, buyer.AccessToken, l.ID, "Còn không bạn?")
	if m := phones.wait(t, sellerPhone, 4); m.Tag != "conversation:"+c.ID || !strings.HasSuffix(m.Body, ": Còn không bạn?") {
		t.Fatalf("the chat push: %+v", m)
	}
	if got = notices(t, seller.AccessToken, 2); len(got.Items) != 2 {
		t.Fatalf("a message lists nothing: %+v", got)
	}

	res := call(t, server, "GET", "/v1/screens/home?near_commune=00004", nil, bearer(buyer.AccessToken))
	if res.Status != http.StatusOK || !strings.Contains(string(res.Body), `"notifications_unread":0`) {
		t.Fatalf("home: %d %s", res.Status, res.Body)
	}

	if r := call(t, server, "POST", "/v1/auth/logout", nil, bearer(seller.AccessToken)); r.Status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", r.Status, r.Body)
	}
	before := phones.count(sellerPhone)
	sendChat(t, buyer.AccessToken, c.ID, map[string]any{"text": "Bạn ơi?"})
	time.Sleep(2 * time.Second)
	if phones.count(sellerPhone) != before {
		t.Fatalf("a logged-out session still gets pushes: %d → %d", before, phones.count(sellerPhone))
	}
}

func TestNotificationRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	for name, c := range map[string]struct {
		method, path string
		body         any
		token        string
		status       int
	}{
		"list signed out":        {"GET", "/v1/notifications", nil, "", 401},
		"list a bad cursor":      {"GET", "/v1/notifications?cursor=x", nil, me.AccessToken, 400},
		"read signed out":        {"POST", "/v1/notifications/" + uuid.NewV7().String() + "/read", nil, "", 401},
		"read a bad id":          {"POST", "/v1/notifications/x/read", nil, me.AccessToken, 400},
		"read all signed out":    {"POST", "/v1/notifications/read-all", nil, "", 401},
		"put signed out":         {"PUT", "/v1/me/push-token", map[string]any{"token": "t"}, "", 401},
		"put an empty token":     {"PUT", "/v1/me/push-token", map[string]any{"token": " "}, me.AccessToken, 422},
		"put an unknown field":   {"PUT", "/v1/me/push-token", map[string]any{"token": "t", "x": 1}, me.AccessToken, 400},
		"delete signed out":      {"DELETE", "/v1/me/push-token", nil, "", 401},
		"delete without a token": {"DELETE", "/v1/me/push-token", nil, me.AccessToken, 204},
	} {
		var headers [][2]string
		if c.token != "" {
			headers = append(headers, bearer(c.token))
		}
		if res := call(t, server, c.method, c.path, c.body, headers...); res.Status != c.status {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
}
