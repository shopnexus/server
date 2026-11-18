package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/testenv"
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

// putToken gives the caller's session a push token and turns quiet hours
// off, so a test that waits for a push passes at any hour.
func putToken(t *testing.T, token, push string) {
	t.Helper()
	expectStatus(t, call(t, "PUT", "/v1/me/push-token", map[string]any{"token": push}, bearer(token)), http.StatusNoContent)
	loud := defaultSettings()
	loud["quiet"] = map[string]any{"on": false, "from": "22:00", "to": "07:00"}
	expectStatus(t, call(t, "PUT", "/v1/me/notification-settings", loud, bearer(token)), http.StatusOK)
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
	expect[readAllResult](t, call(t, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)), http.StatusOK)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)

	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	got := notices(t, seller.AccessToken, 2)
	n := got.Items[0]
	if n.Type != "offer" || !n.Unread || got.Unread != 1 || n.Target.Kind != "deal" || n.Target.ID != d.ID ||
		!strings.Contains(n.Text, "1 tr") {
		t.Fatalf("the seller's notice: %+v", got)
	}
	if m := phones.wait(t, sellerPhone, 2); m.Data["notification_id"] != n.ID || m.Tag != "notification:"+n.ID {
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
	expect[readAllResult](t, call(t, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)), http.StatusOK)
	if got = notices(t, seller.AccessToken, 2); got.Unread != 0 || got.Items[0].Unread {
		t.Fatalf("after reading: %+v", got)
	}
	marked := got.Items[0].ID
	expectStatus(t, call(t, "POST", "/v1/notifications/unread", map[string]any{"ids": []string{marked}}, bearer(seller.AccessToken)), http.StatusNoContent)
	if got = notices(t, seller.AccessToken, 2); got.Unread != 1 || !got.Items[0].Unread {
		t.Fatalf("after marking one unread: %+v", got)
	}
	undo := expect[readAllResult](t, call(t, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)), http.StatusOK)
	if len(undo.IDs) != 1 || undo.IDs[0] != marked {
		t.Fatalf("read-all's IDs: %+v", undo)
	}
	expectStatus(t, call(t, "POST", "/v1/notifications/unread", map[string]any{"ids": undo.IDs}, bearer(seller.AccessToken)), http.StatusNoContent)
	if got = notices(t, seller.AccessToken, 2); got.Unread != 1 {
		t.Fatalf("after the undo: %+v", got)
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

type readAllResult struct {
	IDs []string `json:"ids"`
}

// manyIDs is n new IDs.
func manyIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = uuid.NewV7().String()
	}
	return out
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
		"unread signed out":      {"POST", "/v1/notifications/unread", map[string]any{"ids": []string{uuid.NewV7().String()}}, "", 401},
		"unread no IDs":          {"POST", "/v1/notifications/unread", map[string]any{"ids": []string{}}, me.AccessToken, 400},
		"unread 501 IDs":         {"POST", "/v1/notifications/unread", map[string]any{"ids": manyIDs(501)}, me.AccessToken, 400},
		"unread a bad ID":        {"POST", "/v1/notifications/unread", map[string]any{"ids": []string{"x"}}, me.AccessToken, 400},
		"put signed out":         {"PUT", "/v1/me/push-token", map[string]any{"token": "t"}, "", 401},
		"put an empty token":     {"PUT", "/v1/me/push-token", map[string]any{"token": " "}, me.AccessToken, 422},
		"put an unknown field":   {"PUT", "/v1/me/push-token", map[string]any{"token": "t", "x": 1}, me.AccessToken, 400},
		"delete signed out":      {"DELETE", "/v1/me/push-token", nil, "", 401},
		"delete without a token": {"DELETE", "/v1/me/push-token", nil, me.AccessToken, 204},
	} {
		check(t, name, call(t, c.method, c.path, c.body, bearer(c.token)), c.status, "")
	}
}

// newestNotice polls token's list until its newest notice is an unread one
// of type typ, and returns the list.
func newestNotice(t *testing.T, token, typ string) notificationList {
	t.Helper()
	var l notificationList
	testenv.Eventually(t, 30*time.Second, func() error {
		res := call(t, "GET", "/v1/notifications", nil, bearer(token))
		if l = expect[notificationList](t, res, http.StatusOK); len(l.Items) == 0 || l.Items[0].Type != typ || !l.Items[0].Unread {
			return fmt.Errorf("no unread %s on top: %s", typ, res.Body)
		}
		return nil
	})
	return l
}

// A buyer cancels an order and asks again on the same deal. The seller's
// notices about the two orders open each its own order, as their pushes
// do, and the earlier one's screen shows the cancelled order.
func TestOrderNoticesOpenTheirOwnOrder(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	sellerPhone := "seller-" + uuid.NewV7().String()
	putToken(t, seller.AccessToken, sellerPhone)
	first := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	newestNotice(t, seller.AccessToken, "cod_requested")
	expect[readAllResult](t, call(t, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)), http.StatusOK)
	moved(t, buyer.AccessToken, first.ID, "cancel", nil)
	newestNotice(t, seller.AccessToken, "order_cancelled")
	expect[readAllResult](t, call(t, "POST", "/v1/notifications/read-all", nil, bearer(seller.AccessToken)), http.StatusOK)
	moved(t, buyer.AccessToken, first.ID, "accept", map[string]any{"price": 1_500_000})
	second := moved(t, buyer.AccessToken, first.ID, "cod", codBody(deliveryAddress(t, buyer.AccessToken)))
	if second.Order.ID == first.Order.ID {
		t.Fatalf("one order: %s", first.Order.ID)
	}

	l := newestNotice(t, seller.AccessToken, "cod_requested")
	want := []struct{ typ, order string }{
		{"cod_requested", second.Order.ID}, {"order_cancelled", first.Order.ID}, {"cod_requested", first.Order.ID},
	}
	if len(l.Items) < len(want) {
		t.Fatalf("%+v", l)
	}
	for i, w := range want {
		if n := l.Items[i]; n.Type != w.typ || n.Target.Kind != "order" || n.Target.ID != w.order {
			t.Errorf("notice %d: %+v, want %s on %s", i, n, w.typ, w.order)
		}
	}
	testenv.Eventually(t, 30*time.Second, func() error {
		phones.mu.Lock()
		defer phones.mu.Unlock()
		for _, m := range phones.sent[sellerPhone] {
			if m.Data["type"] == "cod_requested" && m.Data["notification_id"] == l.Items[0].ID {
				if m.Data["target_kind"] != "order" || m.Data["target_id"] != second.Order.ID {
					t.Fatalf("the second order's push: %+v", m.Data)
				}
				return nil
			}
		}
		return fmt.Errorf("no push of notice %s", l.Items[0].ID)
	})
	if s := orderScreen(t, seller.AccessToken, l.Items[1].Target.ID); s.Order.ID != first.Order.ID || s.Order.Status != "cancelled" {
		t.Fatalf("the earlier notice's order: %+v", s.Order)
	}
}
