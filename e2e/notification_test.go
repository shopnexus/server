package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

type notificationView struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Text   string `json:"text"`
	Unread bool   `json:"unread"`
	Target struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	} `json:"target"`
}

type notificationList struct {
	page[notificationView]

	Unread int `json:"unread"`
}

// notices polls token's list until it holds n notices.
func notices(t *testing.T, token string, n int) notificationList {
	t.Helper()
	var l notificationList
	testenv.Eventually(t, 30*time.Second, func() error {
		res := call(t, "GET", "/v1/notifications", nil, bearer(token))
		if l = expect[notificationList](t, res, http.StatusOK); len(l.Items) < n {
			return fmt.Errorf("%d notices, want %d: %s", len(l.Items), n, res.Body)
		}
		return nil
	})
	return l
}

func putToken(t *testing.T, token, push string) {
	t.Helper()
	expectStatus(t, call(t, "PUT", "/v1/me/push-token", map[string]any{"token": push}, bearer(token)), http.StatusNoContent)
}

func TestNotifications(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	sellerPhone, buyerPhone := "seller-"+uuid.NewV7().String(), "buyer-"+uuid.NewV7().String()
	putToken(t, seller.AccessToken, sellerPhone)
	putToken(t, buyer.AccessToken, buyerPhone)
	l := activeListing(t, seller.AccessToken, nil)
	if live := notices(t, seller.AccessToken, 1); live.Items[0].Type != "listing_live" || live.Items[0].Target.ID != l.ID {
		t.Fatalf("the listing going live: %+v", live)
	}
	phones.wait(t, sellerPhone, 1)
	expectStatus(t, call(t, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)), http.StatusNoContent)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)

	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	got := notices(t, seller.AccessToken, 2)
	n := got.Items[0]
	if n.Type != "offer" || !n.Unread || got.Unread != 1 || n.Target.Kind != "deal" || n.Target.ID != d.ID ||
		!strings.Contains(n.Text, "1 tr") {
		t.Fatalf("the seller's notice: %+v", got)
	}
	if m := phones.wait(t, sellerPhone, 2); m.Data["notification_id"] != n.ID || m.Tag != "deal:"+d.ID {
		t.Fatalf("the seller's push: %+v", m)
	}

	moved(t, buyer.AccessToken, d.ID, "withdraw", nil)
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_100_000})
	phones.wait(t, sellerPhone, 3)
	got = notices(t, seller.AccessToken, 2)
	if len(got.Items) != 2 || got.Unread != 1 || !strings.Contains(got.Items[0].Text, "1,1 tr") {
		t.Fatalf("the second offer replaces the unread notice: %+v", got)
	}

	path := "/v1/notifications/" + got.Items[0].ID + "/read"
	if res := call(t, "POST", path, nil, bearer(stranger.AccessToken)); res.Status != http.StatusNotFound ||
		errorOf(t, res).Code != "notification_not_found" {
		t.Fatalf("a stranger reads it: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "POST", path, nil, bearer(seller.AccessToken)), http.StatusNoContent)
	expectStatus(t, call(t, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)), http.StatusNoContent)
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

	res := call(t, "GET", "/v1/screens/home?near_commune=00004", nil, bearer(buyer.AccessToken))
	if res.Status != http.StatusOK || !strings.Contains(string(res.Body), `"notifications_unread":0`) {
		t.Fatalf("home: %d %s", res.Status, res.Body)
	}

	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/current", nil, bearer(seller.AccessToken)), http.StatusNoContent)
	before := phones.count(sellerPhone)
	sendChat(t, buyer.AccessToken, c.ID, map[string]any{"text": "Bạn ơi?"})
	time.Sleep(2 * time.Second)
	if phones.count(sellerPhone) != before {
		t.Fatalf("a signed-out session still gets pushes: %d → %d", before, phones.count(sellerPhone))
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
		check(t, name, call(t, c.method, c.path, c.body, bearer(c.token)), c.status, "")
	}
}
