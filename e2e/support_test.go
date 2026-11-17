package e2e

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/platform/stream"
	"github.com/shopnexus/server2/internal/platform/testenv"
)

// newID is an id nothing has.
func newID() string { return uuid.NewV7().String() }

// errQueue is a wait that has not seen its state yet.
var errQueue = errors.New("not yet")

// shopNexusID is the system account every support chat is with.
const shopNexusID = "0193e000-0000-7000-8000-000000000001"

// ticketBody is the Ticket schema, as far as the tests read it.
type ticketBody struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	ConversationID string `json:"conversation_id"`
	FirstMessageID string `json:"first_message_id"`
	Order          *struct {
		TrackingCode string `json:"tracking_code"`
	} `json:"order"`
}

func openTicket(t *testing.T, token string, body map[string]any, status int) ticketBody {
	t.Helper()
	return expect[ticketBody](t, call(t, "POST", "/v1/support/tickets", body, bearer(token), newKey()), status)
}

func TestSupportFromReportToReply(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	putToken(t, buyer.AccessToken, "phone-"+buyer.Account.ID)
	l := activeListing(t, seller.AccessToken, nil)
	order := shippedOrder(t, seller.AccessToken, buyer.AccessToken, l.ID).Order
	report := map[string]any{"kind": "not_received", "order_id": order.ID, "body": "GHTK báo đã giao mà mình chưa nhận"}
	tk := openTicket(t, buyer.AccessToken, report, http.StatusCreated)
	if tk.Kind != "not_received" || tk.Status != "open" || tk.Order == nil || tk.Order.TrackingCode == "" || tk.FirstMessageID == "" {
		t.Fatalf("ticket %+v", tk)
	}
	if again := openTicket(t, buyer.AccessToken, report, http.StatusOK); again.ID != tk.ID {
		t.Fatalf("a second report: %s", again.ID)
	}
	check(t, "the seller reports", call(t, "POST", "/v1/support/tickets", report, bearer(seller.AccessToken), newKey()), 404, "order_not_found")
	if got := expect[ticketBody](t, call(t, "GET", "/v1/support/orders/"+order.ID+"/ticket", nil, bearer(buyer.AccessToken)), 200); got.ID != tk.ID {
		t.Fatalf("the order's ticket: %s", got.ID)
	}
	screen := expect[struct {
		Other struct {
			Official bool `json:"official"`
		} `json:"other"`
		Ticket *ticketBody `json:"ticket"`
	}](t, call(t, "GET", "/v1/screens/chat/"+tk.ConversationID, nil, bearer(buyer.AccessToken)), http.StatusOK)
	if screen.Ticket == nil || screen.Ticket.ID != tk.ID || !screen.Other.Official {
		t.Fatalf("chat screen %+v", screen)
	}

	selected := orderScreen(t, buyer.AccessToken, order.ID)
	if selectedTicket := decode[ticketBody](t, Response{Body: selected.Ticket}); selectedTicket.ID != tk.ID {
		t.Fatalf("selected order ticket: %s", selected.Ticket)
	}
	readUpdates := func(token string) stream.Page {
		t.Helper()
		return expect[stream.Page](t, call(t, "GET", "/v1/updates?since=0", nil, bearer(token)), 200)
	}
	countTicketHints := func(updates stream.Page) int {
		n := 0
		for _, u := range updates.Updates {
			if u.Kind == stream.KindTicket && u.ConversationID != nil && u.ConversationID.String() == tk.ConversationID {
				n++
			}
		}
		return n
	}
	if n := countTicketHints(readUpdates(buyer.AccessToken)); n != 2 {
		t.Fatalf("creation hints: %d", n)
	}
	if n := countTicketHints(readUpdates(seller.AccessToken)); n != 0 {
		t.Fatalf("seller ticket hints: %d", n)
	}
	mod := signInMember(t, consoledomain.RoleModerator)
	var queued bool
	testenv.Eventually(t, 10*time.Second, func() error {
		q := expect[struct {
			Items []struct {
				Ticket   ticketBody `json:"ticket"`
				LastFrom string     `json:"last_from"`
			} `json:"items"`
		}](t, consoleCall(t, mod, "GET", "/v1/console/support/tickets?status=open&kind=not_received&limit=100", nil), http.StatusOK)
		for _, it := range q.Items {
			queued = queued || (it.Ticket.ID == tk.ID && it.LastFrom == "person")
		}
		if !queued {
			return errQueue
		}
		return nil
	})
	expect[map[string]any](t, consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/replies",
		map[string]any{"body": "Chào bạn, mình đã xem lịch trình GHTK", "reply_to": tk.FirstMessageID}, newKey()), http.StatusCreated)
	msgs := expect[page[struct {
		SenderID string `json:"sender_id"`
		Text     string `json:"text"`
		ReplyTo  *struct {
			ID string `json:"id"`
		} `json:"reply_to"`
	}]](t, call(t, "GET", "/v1/conversations/"+tk.ConversationID+"/messages", nil, bearer(buyer.AccessToken)), http.StatusOK)
	if msgs.Items[0].SenderID != shopNexusID || msgs.Items[0].ReplyTo == nil || msgs.Items[0].ReplyTo.ID != tk.FirstMessageID {
		t.Fatalf("the reply %+v", msgs.Items[0])
	}
	if m := phones.waitTitled(t, "phone-"+buyer.Account.ID, "ShopNexus"); m.Data["type"] != "support_message" ||
		m.Data["target_kind"] != "ticket" || m.Data["target_id"] != tk.ID {
		t.Fatalf("push %+v", m)
	}
	summary := expect[struct {
		Open   int `json:"open"`
		Unread int `json:"unread"`
	}](t, call(t, "GET", "/v1/support/summary", nil, bearer(buyer.AccessToken)), http.StatusOK)
	if summary.Open != 1 || summary.Unread != 1 {
		t.Fatalf("summary %+v", summary)
	}

	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/resolve", nil), http.StatusOK)
	check(t, "resolve twice", consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/resolve", nil), 409, "ticket_not_allowed")
	expectStatus(t, call(t, "POST", "/v1/conversations/"+tk.ConversationID+"/messages", map[string]any{"text": "Vẫn chưa nhận"},
		bearer(buyer.AccessToken), newKey()), http.StatusCreated)
	testenv.Eventually(t, 10*time.Second, func() error {
		d := expect[struct {
			Ticket struct {
				Ticket ticketBody `json:"ticket"`
			} `json:"ticket"`
		}](t, consoleCall(t, mod, "GET", "/v1/console/support/tickets/"+tk.ID, nil), http.StatusOK)
		if d.Ticket.Ticket.Status != "open" {
			return errQueue
		}
		return nil
	})
	updates := readUpdates(buyer.AccessToken)
	if n := countTicketHints(updates); n != 4 {
		t.Fatalf("resolve/reopen hints: %d", n)
	}
	var lastMessage, lastTicket int64
	for _, u := range updates.Updates {
		if u.ConversationID == nil || u.ConversationID.String() != tk.ConversationID {
			continue
		}
		if u.Kind == stream.KindMessage {
			lastMessage = u.Seq
		}
		if u.Kind == stream.KindTicket {
			lastTicket = u.Seq
		}
	}
	if lastTicket <= lastMessage {
		t.Fatalf("reopen hint %d did not follow message %d", lastTicket, lastMessage)
	}
	if res := consoleCall(t, mod, "GET", "/v1/console/actions?target_id="+tk.ID, nil); res.Status != http.StatusForbidden &&
		!strings.Contains(string(res.Body), "ticket.resolve") {
		t.Fatalf("audit: %d %s", res.Status, res.Body)
	}
}

func TestCarrierRequestGetsItsNote(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	tk := openTicket(t, seller.AccessToken, map[string]any{"kind": "carrier_request", "carrier_name": "Ninja Van"}, http.StatusCreated)
	mod := signInMember(t, consoledomain.RoleModerator)
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/resolve", nil), http.StatusOK)
	msgs := expect[page[struct {
		Text string `json:"text"`
	}]](t, call(t, "GET", "/v1/conversations/"+tk.ConversationID+"/messages", nil, bearer(seller.AccessToken)), http.StatusOK)
	if msgs.Items[0].Text != "Đã ghi nhận hãng Ninja Van." {
		t.Fatalf("%+v", msgs.Items)
	}
}

func TestSupportRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	check(t, "other without a body", call(t, "POST", "/v1/support/tickets", map[string]any{"kind": "other"}, bearer(me.AccessToken), newKey()),
		422, "invalid_ticket")
	check(t, "no key", call(t, "POST", "/v1/support/tickets", map[string]any{"kind": "other", "body": "x"}, bearer(me.AccessToken)), 400, "")
	check(t, "signed out", call(t, "POST", "/v1/support/tickets", map[string]any{"kind": "other", "body": "x"}, newKey()), 401, "unauthenticated")
	for range 10 {
		openTicket(t, me.AccessToken, map[string]any{"kind": "other", "body": "x"}, http.StatusCreated)
	}
	check(t, "the eleventh", call(t, "POST", "/v1/support/tickets", map[string]any{"kind": "other", "body": "x"}, bearer(me.AccessToken), newKey()),
		429, "too_many_tickets")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	openTicket(t, suspended.AccessToken, map[string]any{"kind": "other", "body": "Vì sao tôi bị khoá?"}, http.StatusCreated)
	check(t, "block ShopNexus", call(t, "POST", "/v1/blocks", map[string]any{"account_id": shopNexusID}, bearer(me.AccessToken)), 409, "account_is_system")
	mod := signInMember(t, consoledomain.RoleModerator)
	check(t, "suspend ShopNexus", consoleCall(t, mod, "POST", "/v1/console/accounts/"+shopNexusID+"/suspend", map[string]any{"reason": "thử"}),
		409, "account_is_system")
	expectStatus(t, consoleCall(t, consoleSession{}, "GET", "/v1/console/support/tickets?status=open", nil), 401)
}

func TestSupportStaysOutOfTinNhan(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	tk := openTicket(t, me.AccessToken, map[string]any{"kind": "other", "body": "Hỏi về khu vực"}, http.StatusCreated)
	mod := signInMember(t, consoledomain.RoleModerator)
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/replies", map[string]any{"body": "Chào bạn"}, newKey()),
		http.StatusCreated)
	if rows := inboxRows(t, me.AccessToken); len(rows) != 0 {
		t.Fatalf("inbox %+v", rows)
	}
	if u := expect[map[string]int](t, call(t, "GET", "/v1/me/unread", nil, bearer(me.AccessToken)), http.StatusOK); u["unread"] != 0 {
		t.Fatalf("unread %v", u)
	}
	check(t, "archive", call(t, "POST", "/v1/conversations/"+tk.ConversationID+"/archive", nil, bearer(me.AccessToken)), 409,
		"conversation_is_support")
	mine := expect[struct {
		Items []struct {
			Ticket ticketBody `json:"ticket"`
			Unread int        `json:"unread"`
		} `json:"items"`
	}](t, call(t, "GET", "/v1/support/tickets", nil, bearer(me.AccessToken)), http.StatusOK)
	if len(mine.Items) != 1 || mine.Items[0].Ticket.ID != tk.ID || mine.Items[0].Unread != 1 {
		t.Fatalf("mine %+v", mine)
	}
}

func TestReplyBySwipeQuotes(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	c := openChat(t, buyer.AccessToken, l.ID, "Còn bảo hành không?")
	first := expect[page[messageBody]](t, call(t, "GET", "/v1/conversations/"+c.ID+"/messages", nil, bearer(seller.AccessToken)), http.StatusOK).Items[0]
	m := expect[struct {
		ReplyTo *struct {
			Text string `json:"text"`
		} `json:"reply_to"`
	}](t, call(t, "POST", "/v1/conversations/"+c.ID+"/messages", map[string]any{"text": "Còn 3 tháng", "reply_to": first.ID},
		bearer(seller.AccessToken), newKey()), http.StatusCreated)
	if m.ReplyTo == nil || m.ReplyTo.Text != "Còn bảo hành không?" {
		t.Fatalf("%+v", m)
	}
	check(t, "a quote of another chat", call(t, "POST", "/v1/conversations/"+c.ID+"/messages",
		map[string]any{"text": "x", "reply_to": c.ID}, bearer(seller.AccessToken), newKey()), 422, "invalid_message")
}

// TestSupportStatuses walks the statuses the other tests leave out.
func TestSupportStatuses(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	unknown := "/v1/console/support/tickets/" + newID()
	for _, p := range []string{"/v1/support/tickets", "/v1/support/summary", "/v1/support/orders/" + newID() + "/ticket"} {
		check(t, "signed out "+p, call(t, "GET", p, nil), 401, "unauthenticated")
	}
	check(t, "no ticket for the order", call(t, "GET", "/v1/support/orders/"+newID()+"/ticket", nil, bearer(me.AccessToken)), 404, "ticket_not_found")
	check(t, "an unknown order", call(t, "POST", "/v1/support/tickets", map[string]any{"kind": "not_received", "order_id": newID()},
		bearer(me.AccessToken), newKey()), 404, "order_not_found")
	seller, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	pending := pendingOrder(t, me.AccessToken, l.ID).Order
	check(t, "a pending order", call(t, "POST", "/v1/support/tickets", map[string]any{"kind": "not_received", "order_id": pending.ID},
		bearer(me.AccessToken), newKey()), 409, "ticket_not_allowed")

	tk := openTicket(t, me.AccessToken, map[string]any{"kind": "other", "body": "Hỏi"}, http.StatusCreated)
	for _, a := range []string{"unread", "unarchive"} {
		check(t, a, call(t, "POST", "/v1/conversations/"+tk.ConversationID+"/"+a, nil, bearer(me.AccessToken)), 409, "conversation_is_support")
	}
	check(t, "report ShopNexus", call(t, "POST", "/v1/reports", map[string]any{"account_id": shopNexusID, "reason": "scam"},
		bearer(me.AccessToken)), 409, "account_is_system")

	mod := signInMember(t, consoledomain.RoleModerator)
	check(t, "unsuspend ShopNexus", consoleCall(t, mod, "POST", "/v1/console/accounts/"+shopNexusID+"/unsuspend", nil), 409, "account_is_system")
	expectStatus(t, consoleCall(t, mod, "GET", "/v1/console/support/summary", nil), http.StatusOK)
	expectStatus(t, consoleCall(t, mod, "GET", "/v1/console/support/tickets/"+tk.ID+"/messages", nil), http.StatusOK)
	check(t, "reopen an open ticket", consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/reopen", nil), 409, "ticket_not_allowed")
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/resolve", nil), http.StatusOK)
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/reopen", nil), http.StatusOK)
	check(t, "an empty reply", consoleCall(t, mod, "POST", "/v1/console/support/tickets/"+tk.ID+"/replies", map[string]any{"body": " "}, newKey()),
		422, "invalid_message")
	for _, c := range []struct{ method, path string }{
		{"GET", unknown}, {"GET", unknown + "/messages"}, {"POST", unknown + "/resolve"}, {"POST", unknown + "/reopen"},
	} {
		check(t, c.method+" "+c.path, consoleCall(t, mod, c.method, c.path, nil), 404, "ticket_not_found")
	}
	check(t, "reply to no ticket", consoleCall(t, mod, "POST", unknown+"/replies", map[string]any{"body": "x"}, newKey()), 404, "ticket_not_found")
	for _, c := range []struct{ method, path string }{
		{"GET", "/v1/console/support/summary"},
		{"GET", unknown},
		{"GET", unknown + "/messages"},
		{"POST", unknown + "/resolve"},
		{"POST", unknown + "/reopen"},
		{"POST", unknown + "/replies"},
	} {
		expectStatus(t, consoleCall(t, consoleSession{}, c.method, c.path, map[string]any{"body": "x"}, newKey()), 401)
	}
}
