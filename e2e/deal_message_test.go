package e2e

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

// dealMoves waits until token's first page of conversation id holds n deal
// messages and returns their move kinds, newest first, with the page.
func dealMoves(t *testing.T, token, id string, n int) ([]string, []messageBody) {
	t.Helper()
	var (
		kinds []string
		items []messageBody
	)
	testenv.Eventually(t, 15*time.Second, func() error {
		kinds, items = nil, chatMessages(t, token, id)
		for _, m := range items {
			if m.Deal != nil {
				kinds = append(kinds, m.Deal.Move.Kind)
			}
		}
		if len(kinds) != n {
			return fmt.Errorf("%d deal messages %v, want %d", len(kinds), kinds, n)
		}
		return nil
	})
	return kinds, items
}

// Opening a deal opens the pair's conversation, and each move writes a deal
// message there: the other member gets the message update, counts it
// unread and sees it as the inbox's last message; the chat view shows it,
// the block and the other member's read marker.
func TestDealMessages(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	sock := openSocket(t, seller.AccessToken, "")
	sock.ready(t)

	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	if d.ConversationID == "" {
		t.Fatalf("%+v", d)
	}
	if got := chatMessages(t, buyer.AccessToken, d.ConversationID); len(got) != 0 {
		t.Fatalf("messages before a move: %+v", got)
	}
	if again := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, http.StatusCreated); again.ConversationID != d.ConversationID {
		t.Fatalf("a second deal of the pair: %s, want %s", again.ConversationID, d.ConversationID)
	}

	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	_, items := dealMoves(t, seller.AccessToken, d.ConversationID, 1)
	offer := items[0]
	if offer.SenderID != buyer.Account.ID || offer.Text != "" || offer.Status != "sent" || offer.Deal.DealID != d.ID ||
		offer.Deal.Move.By != "buyer" || offer.Deal.Move.Amount != 1_000_000 {
		t.Fatalf("%+v %+v", offer, offer.Deal)
	}
	sock.waitFrame(t, "update", "message", offer.ID)
	row := inboxRows(t, seller.AccessToken)[buyer.Account.ID]
	if row.ID != d.ConversationID || row.Unread != 1 || row.LastMessage.ID != offer.ID || row.LastMessage.Deal == nil ||
		row.Deal == nil || row.Deal.ConversationID != d.ConversationID {
		t.Fatalf("the seller's row: %+v", row)
	}
	if row := inboxRows(t, buyer.AccessToken)[seller.Account.ID]; row.Unread != 0 {
		t.Fatalf("the buyer's own move is unread: %+v", row)
	}

	moved(t, seller.AccessToken, d.ID, "offer", map[string]any{"amount": 1_300_000})
	moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_300_000})
	kinds, items := dealMoves(t, buyer.AccessToken, d.ConversationID, 3)
	if !slices.Equal(kinds, []string{"accept", "counter", "offer"}) {
		t.Fatalf("kinds %v", kinds)
	}
	path := "/v1/screens/chat/" + d.ConversationID
	view := expect[chatScreen](t, call(t, "GET", path, nil, bearer(buyer.AccessToken)), http.StatusOK)
	if view.Blocked || view.OtherRead != "" || len(view.Messages.Items) != 3 || view.Messages.Items[0].ID != items[0].ID {
		t.Fatalf("the buyer's view: %+v", view)
	}
	expectStatus(t, call(t, "POST", "/v1/conversations/"+d.ConversationID+"/read", map[string]any{"message_id": items[0].ID},
		bearer(seller.AccessToken)), http.StatusNoContent)
	if view = expect[chatScreen](t, call(t, "GET", path, nil, bearer(buyer.AccessToken)), http.StatusOK); view.OtherRead != items[0].ID {
		t.Fatalf("the seller's marker: %q, want %s", view.OtherRead, items[0].ID)
	}

	check(t, "send a deal message", call(t, "POST", "/v1/conversations/"+d.ConversationID+"/messages",
		map[string]any{"deal": map[string]any{"deal_id": d.ID}}, bearer(buyer.AccessToken), newKey()), 400, "")

	// A move across a block still writes its message; the blocker's inbox
	// leaves the conversation out.
	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": buyer.Account.ID}, bearer(seller.AccessToken)),
		http.StatusNoContent)
	for _, token := range []string{buyer.AccessToken, seller.AccessToken} {
		if view = expect[chatScreen](t, call(t, "GET", path, nil, bearer(token)), http.StatusOK); !view.Blocked {
			t.Fatalf("not blocked: %+v", view)
		}
	}
	moved(t, buyer.AccessToken, d.ID, "cancel", nil)
	if kinds, _ = dealMoves(t, buyer.AccessToken, d.ConversationID, 4); kinds[0] != "cancel" {
		t.Fatalf("kinds %v", kinds)
	}
	if _, listed := inboxRows(t, seller.AccessToken)[buyer.Account.ID]; listed {
		t.Fatal("the blocker's inbox lists the conversation")
	}
}

// Each order step writes a deal message: the buyer's COD request, the
// seller's shipment, and the carrier's report from ShopNexus, which counts
// as unread for both.
func TestOrderStepMessages(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	code := trackingCode()
	pending := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	orderMoved(t, seller.AccessToken, pending.Order.ID, "shipment", shipmentBody(code))
	expectStatus(t, setShipmentStatus(t, code, "in_transit"), http.StatusNoContent)
	kinds, items := dealMoves(t, buyer.AccessToken, pending.ConversationID, 4)
	if !slices.Equal(kinds, []string{"shipment_updated", "shipped", "cod_requested", "accept"}) {
		t.Fatalf("kinds %v", kinds)
	}
	senders := []string{shopNexusID, seller.Account.ID, buyer.Account.ID, buyer.Account.ID}
	for i, m := range items {
		if m.SenderID != senders[i] || (i == 0 && m.Deal.Move.By != "system") {
			t.Errorf("%s: %+v %+v", kinds[i], m, m.Deal)
		}
	}
	if row := inboxRows(t, buyer.AccessToken)[seller.Account.ID]; row.Unread != 2 || row.LastMessage.ID != items[0].ID {
		t.Fatalf("the buyer's row: %+v", row)
	}
	if row := inboxRows(t, seller.AccessToken)[buyer.Account.ID]; row.Unread != 3 {
		t.Fatalf("the seller's row: %+v", row)
	}
}
