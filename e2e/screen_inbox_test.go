package e2e

import (
	"net/http"
	"net/url"
	"testing"
	"uuid"
)

type inboxScreen struct {
	page[inboxRow]

	Unread int `json:"unread"`
}

type inboxRow struct {
	ID    string `json:"id"`
	Other struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	} `json:"other"`
	Unread   int  `json:"unread"`
	Archived bool `json:"archived"`
	Deal     *struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Status    string `json:"status"`
		YourTurn  bool   `json:"your_turn"`
		OpenPrice *struct {
			By string `json:"by"`
		} `json:"open_price"`
		Listing struct {
			ID string `json:"id"`
		} `json:"listing"`
	} `json:"deal"`
	OtherActiveDeals int          `json:"other_active_deals"`
	ScamWarning      *scamWarning `json:"scam_warning"`
}

type chatScreen struct {
	ID    string `json:"id"`
	Other struct {
		ID string `json:"id"`
	} `json:"other"`
	Archived    bool              `json:"archived"`
	Deals       []dealView        `json:"deals"`
	Messages    page[messageBody] `json:"messages"`
	ScamWarning *scamWarning      `json:"scam_warning"`
}

type scamWarning struct {
	Signals []struct {
		Code string `json:"code"`
		Text string `json:"text"`
	} `json:"signals"`
	Dismissed bool `json:"dismissed"`
}

// codes lists a warning's codes.
func (w *scamWarning) codes() []string {
	out := make([]string, 0, len(w.Signals))
	for _, s := range w.Signals {
		out = append(out, s.Code)
	}
	return out
}

// inboxView reads a page of token's inbox view; query is the query string
// without its "?".
func inboxView(t *testing.T, token, query string) inboxScreen {
	t.Helper()
	return expect[inboxScreen](t, call(t, "GET", "/v1/screens/inbox?"+query, nil, bearer(token)), http.StatusOK)
}

// inboxRows reads the first page of token's inbox view by the other
// member's ID.
func inboxRows(t *testing.T, token string) map[string]inboxRow {
	t.Helper()
	rows := map[string]inboxRow{}
	for _, r := range inboxView(t, token, "").Items {
		rows[r.Other.ID] = r
	}
	return rows
}

func TestInboxDeal(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	asker, _ := newAccount(t)
	bike, lamp := activeListing(t, seller.AccessToken, nil), activeListing(t, seller.AccessToken, nil)

	openChat(t, buyer.AccessToken, bike.ID, "Bớt không bạn?")
	offered := openDeal(t, buyer.AccessToken, bike.ID, http.StatusCreated)
	moved(t, buyer.AccessToken, offered.ID, "offer", map[string]any{"amount": 1_000_000})
	idle := openDeal(t, buyer.AccessToken, lamp.ID, http.StatusCreated)
	openChat(t, asker.AccessToken, bike.ID, "Còn không bạn?")

	rows := inboxRows(t, seller.AccessToken)
	if r := rows[buyer.Account.ID]; r.Deal == nil || r.Deal.ID != offered.ID || !r.Deal.YourTurn || r.Deal.Role != "seller" ||
		r.Deal.OpenPrice == nil || r.Deal.OpenPrice.By != "buyer" || r.Deal.Listing.ID != bike.ID || r.OtherActiveDeals != 1 {
		t.Fatalf("the seller's row with the buyer: %+v", r)
	}
	if r, ok := rows[asker.Account.ID]; !ok || r.Deal != nil || r.OtherActiveDeals != 0 {
		t.Fatalf("the seller's row with the asker: %+v %v", r, ok)
	}
	// Neither deal waits for the buyer, so the row shows the newer.
	if r := inboxRows(t, buyer.AccessToken)[seller.Account.ID]; r.Deal == nil || r.Deal.ID != idle.ID || r.Deal.YourTurn ||
		r.Deal.Status != "negotiating" || r.OtherActiveDeals != 1 {
		t.Fatalf("the buyer's row: %+v", r)
	}
}

// The inbox view pages through cursor, each page with its rows' deals and
// the caller's whole unread count.
func TestInboxScreenPages(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	buyers := make([]signIn, 0, 3)
	deals := map[string]string{} // buyer → deal
	for range 3 {
		b, _ := newAccount(t)
		openChat(t, b.AccessToken, l.ID, "Còn không bạn?")
		deals[b.Account.ID] = openDeal(t, b.AccessToken, l.ID, http.StatusCreated).ID
		buyers = append(buyers, b)
	}

	first := inboxView(t, seller.AccessToken, "limit=2")
	if len(first.Items) != 2 || first.NextCursor == "" || first.Unread != 3 ||
		first.Items[0].Other.ID != buyers[2].Account.ID || first.Items[1].Other.ID != buyers[1].Account.ID {
		t.Fatalf("first page: %+v", first)
	}
	rest := inboxView(t, seller.AccessToken, "limit=2&cursor="+first.NextCursor)
	if len(rest.Items) != 1 || rest.NextCursor != "" || rest.Unread != 3 || rest.Items[0].Other.ID != buyers[0].Account.ID {
		t.Fatalf("second page: %+v", rest)
	}
	for _, r := range append(first.Items, rest.Items...) {
		if r.Deal == nil || r.Deal.ID != deals[r.Other.ID] || r.Deal.Role != "seller" || r.Unread != 1 || r.Archived {
			t.Errorf("row with %s: %+v", r.Other.ID, r)
		}
	}
	if archived := inboxView(t, seller.AccessToken, "archived=true"); len(archived.Items) != 0 || archived.Unread != 3 {
		t.Errorf("archived: %+v", archived)
	}

	check(t, "signed out", call(t, "GET", "/v1/screens/inbox", nil), 401, "unauthenticated")
	check(t, "a bad flag", call(t, "GET", "/v1/screens/inbox?archived=maybe", nil, bearer(seller.AccessToken)), 400, "")
	check(t, "a bad cursor", call(t, "GET", "/v1/screens/inbox?cursor=x", nil, bearer(seller.AccessToken)), 400, "")
}

// The chat view shows the pair's deals both ways, newest first, with the
// newest messages, and only to the two members.
func TestChatScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	c := openChat(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, "Còn không bạn?")
	sold := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, http.StatusCreated)
	bought := openDeal(t, seller.AccessToken, activeListing(t, buyer.AccessToken, nil).ID, http.StatusCreated)
	reply := sendChat(t, seller.AccessToken, c.ID, map[string]any{"text": "Còn bạn ơi"})
	expectStatus(t, call(t, "POST", "/v1/conversations/"+c.ID+"/archive", nil, bearer(seller.AccessToken)), http.StatusNoContent)

	path := "/v1/screens/chat/" + c.ID
	res := call(t, "GET", path, nil, bearer(seller.AccessToken))
	got := decode[chatScreen](t, res)
	if res.Status != http.StatusOK || got.ID != c.ID || got.Other.ID != buyer.Account.ID || !got.Archived ||
		len(got.Deals) != 2 || got.Deals[0].ID != bought.ID || got.Deals[0].Role != "buyer" || got.Deals[1].ID != sold.ID ||
		len(got.Messages.Items) != 2 || got.Messages.Items[0].ID != reply.ID || got.Messages.NextCursor != "" {
		t.Fatalf("seller's view: %d %s", res.Status, res.Body)
	}
	res = call(t, "GET", path, nil, bearer(buyer.AccessToken))
	if got = decode[chatScreen](t, res); res.Status != http.StatusOK || got.Other.ID != seller.Account.ID || got.Archived ||
		len(got.Deals) != 2 || got.Deals[0].Role != "seller" {
		t.Fatalf("buyer's view: %d %s", res.Status, res.Body)
	}

	check(t, "signed out", call(t, "GET", path, nil), 401, "unauthenticated")
	check(t, "a bad id", call(t, "GET", "/v1/screens/chat/x", nil, bearer(buyer.AccessToken)), 400, "")
	check(t, "a stranger", call(t, "GET", path, nil, bearer(stranger.AccessToken)), 404, "conversation_not_found")
	check(t, "an unknown conversation", call(t, "GET", "/v1/screens/chat/"+uuid.NewV7().String(), nil, bearer(buyer.AccessToken)), 404, "conversation_not_found")
}

type inboxSearch struct {
	Threads  []inboxRow `json:"threads"`
	Messages []struct {
		ConversationID string `json:"conversation_id"`
		Other          struct {
			ID string `json:"id"`
		} `json:"other"`
		Message messageBody `json:"message"`
	} `json:"messages"`
	NextCursor string `json:"next_cursor"`
}

func TestInboxSearch(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, map[string]any{"title": "Giày Nike Air cũ"})
	c := openChat(t, buyer.AccessToken, l.ID, "Giày còn SHIP nhanh không bạn?")
	openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	sendChat(t, seller.AccessToken, c.ID, map[string]any{"text": "Ship được nhé"})
	search := func(token, query string) Response {
		t.Helper()
		return call(t, "GET", "/v1/screens/inbox/search?"+query, nil, bearer(token))
	}

	first := expect[inboxSearch](t, search(seller.AccessToken, "q=ship&limit=1"), http.StatusOK)
	if len(first.Messages) != 1 || first.Messages[0].Message.Text != "Ship được nhé" || first.Messages[0].ConversationID != c.ID ||
		first.Messages[0].Other.ID != buyer.Account.ID || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	rest := expect[inboxSearch](t, search(seller.AccessToken, "q=ship&limit=1&cursor="+first.NextCursor), http.StatusOK)
	if len(rest.Messages) != 1 || rest.Messages[0].Message.Text != "Giày còn SHIP nhanh không bạn?" || rest.NextCursor != "" ||
		rest.Threads == nil || len(rest.Threads) != 0 {
		t.Fatalf("second page: %+v", rest)
	}
	// The listing's title names the thread; it comes with the pair's deal.
	byTitle := expect[inboxSearch](t, search(buyer.AccessToken, "q="+url.QueryEscape("NIKE ai")), http.StatusOK)
	if len(byTitle.Threads) != 1 || byTitle.Threads[0].ID != c.ID || byTitle.Threads[0].Deal == nil {
		t.Fatalf("by title: %+v", byTitle)
	}

	check(t, "signed out", call(t, "GET", "/v1/screens/inbox/search?q=ship", nil), 401, "unauthenticated")
	check(t, "no query", search(seller.AccessToken, ""), 400, "")
	check(t, "a bad cursor", search(seller.AccessToken, "q=ship&cursor=x"), 400, "")
	check(t, "only punctuation", search(seller.AccessToken, "q="+url.QueryEscape("!!!")), 422, "invalid_query")
}
