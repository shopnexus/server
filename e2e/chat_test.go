package e2e

import (
	"net/http"
	"testing"
	"time"
	"uuid"
)

type conversationBody struct {
	ID    string `json:"id"`
	Other struct {
		ID string `json:"id"`
	} `json:"other"`
	Archived bool `json:"archived"`
}

type messageBody struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Photos []struct {
		URL string `json:"url"`
	} `json:"photos"`
}

type messageList struct {
	Items []messageBody `json:"items"`
}

// openChat has token open a conversation from listing with text.
func openChat(t *testing.T, token, listing, text string) conversationBody {
	t.Helper()
	res := call(t, server, "POST", "/v1/conversations", map[string]any{"listing_id": listing, "text": text}, bearer(token), newKey())
	if res.Status != http.StatusCreated {
		t.Fatalf("open: %d %s", res.Status, res.Body)
	}
	return decode[conversationBody](t, res)
}

func sendChat(t *testing.T, token, id string, body map[string]any) messageBody {
	t.Helper()
	res := call(t, server, "POST", "/v1/conversations/"+id+"/messages", body, bearer(token), newKey())
	if res.Status != http.StatusCreated {
		t.Fatalf("send: %d %s", res.Status, res.Body)
	}
	return decode[messageBody](t, res)
}

func chatMessages(t *testing.T, token, id string) []messageBody {
	t.Helper()
	res := call(t, server, "GET", "/v1/conversations/"+id+"/messages", nil, bearer(token))
	if res.Status != http.StatusOK {
		t.Fatalf("messages: %d %s", res.Status, res.Body)
	}
	return decode[messageList](t, res).Items
}

func TestChat(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)
	events := openStream(t, seller.AccessToken)

	c := openChat(t, buyer.AccessToken, l.ID, "Còn không bạn?")
	if c.Other.ID != seller.Account.ID {
		t.Fatalf("%+v", c)
	}
	waitEvent(t, events, "message", c.ID)

	reply := sendChat(t, seller.AccessToken, c.ID, map[string]any{"text": "Còn bạn ơi"})
	if inbox := inboxView(t, buyer.AccessToken, ""); len(inbox.Items) != 1 || inbox.Items[0].Unread != 1 || inbox.Unread != 1 {
		t.Fatalf("inbox: %+v", inbox)
	}
	res := call(t, server, "GET", "/v1/me/unread", nil, bearer(buyer.AccessToken))
	if res.Status != http.StatusOK || decode[map[string]int](t, res)["unread"] != 1 {
		t.Fatalf("unread: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "POST", "/v1/conversations/"+c.ID+"/read", map[string]any{"message_id": reply.ID}, bearer(buyer.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("read: %d %s", res.Status, res.Body)
	}
	res = call(t, server, "GET", "/v1/me/unread", nil, bearer(buyer.AccessToken))
	if decode[map[string]int](t, res)["unread"] != 0 {
		t.Fatalf("unread after reading: %s", res.Body)
	}

	photo := sendChat(t, buyer.AccessToken, c.ID, map[string]any{"text": "Vết này", "photos": []string{upload(t, buyer.AccessToken, photoWithGPS(t))}})
	deadline := time.Now().Add(30 * time.Second)
	for {
		ms := chatMessages(t, seller.AccessToken, c.ID)
		if ms[0].ID == photo.ID {
			if ms[0].Status != "sent" || ms[0].Photos[0].URL == "" {
				t.Fatalf("%+v", ms[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the photo message never reached the seller")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if res = call(t, server, "POST", "/v1/conversations/"+c.ID+"/archive", nil, bearer(seller.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("archive: %d %s", res.Status, res.Body)
	}
	if archived := inboxView(t, seller.AccessToken, "archived=true"); len(archived.Items) != 1 || !archived.Items[0].Archived {
		t.Fatalf("archived: %+v", archived)
	}
	if res = call(t, server, "POST", "/v1/conversations/"+c.ID+"/unarchive", nil, bearer(seller.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("unarchive: %d %s", res.Status, res.Body)
	}

	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	res = call(t, server, "GET", "/v1/screens/chat/"+c.ID, nil, bearer(seller.AccessToken))
	if got := decode[chatScreen](t, res); res.Status != http.StatusOK || len(got.Deals) != 1 || got.Deals[0].ID != d.ID {
		t.Fatalf("chat: %d %s", res.Status, res.Body)
	}
}

func TestChatRefusals(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)
	c := openChat(t, buyer.AccessToken, l.ID, "Còn không?")
	open := func(token string, body any) Response {
		t.Helper()
		headers := [][2]string{newKey()}
		if token != "" {
			headers = append(headers, bearer(token))
		}
		return call(t, server, "POST", "/v1/conversations", body, headers...)
	}
	msg := map[string]any{"listing_id": l.ID, "text": "Còn không?"}

	expect("open signed out", open("", msg), 401, "unauthenticated")
	expect("open a bad id", open(buyer.AccessToken, map[string]any{"listing_id": "x"}), 400, "")
	expect("open an unknown listing", open(buyer.AccessToken, map[string]any{"listing_id": uuid.NewV7().String(), "text": "Hi"}), 404, "listing_not_found")
	expect("open without a phone", open(emailAccount(t).AccessToken, msg), 403, "phone_required")
	expect("open own listing", open(seller.AccessToken, msg), 409, "own_listing")
	expect("open without text", open(stranger.AccessToken, map[string]any{"listing_id": l.ID, "text": " "}), 422, "invalid_message")
	expect("open with another's photo", open(stranger.AccessToken, map[string]any{"listing_id": l.ID, "photos": []string{upload(t, seller.AccessToken, photoWithGPS(t))}}), 422, "invalid_upload")

	expect("messages signed out", call(t, server, "GET", "/v1/conversations/"+c.ID+"/messages", nil), 401, "unauthenticated")
	expect("messages a bad id", call(t, server, "GET", "/v1/conversations/x/messages", nil, bearer(buyer.AccessToken)), 400, "")
	expect("messages as a stranger", call(t, server, "GET", "/v1/conversations/"+c.ID+"/messages", nil, bearer(stranger.AccessToken)), 404, "conversation_not_found")

	send := func(token, id string, body any) Response {
		t.Helper()
		headers := [][2]string{newKey()}
		if token != "" {
			headers = append(headers, bearer(token))
		}
		return call(t, server, "POST", "/v1/conversations/"+id+"/messages", body, headers...)
	}
	hi := map[string]any{"text": "Hi"}
	expect("send signed out", send("", c.ID, hi), 401, "unauthenticated")
	expect("send a bad id", send(buyer.AccessToken, "x", hi), 400, "")
	expect("send as a stranger", send(stranger.AccessToken, c.ID, hi), 404, "conversation_not_found")
	expect("send nothing", send(buyer.AccessToken, c.ID, map[string]any{"text": ""}), 422, "invalid_message")

	for _, action := range []string{"archive", "unarchive"} {
		expect(action+" signed out", call(t, server, "POST", "/v1/conversations/"+c.ID+"/"+action, nil), 401, "unauthenticated")
		expect(action+" a bad id", call(t, server, "POST", "/v1/conversations/x/"+action, nil, bearer(buyer.AccessToken)), 400, "")
		expect(action+" as a stranger", call(t, server, "POST", "/v1/conversations/"+c.ID+"/"+action, nil, bearer(stranger.AccessToken)), 404, "conversation_not_found")
	}
	read := map[string]any{"message_id": uuid.NewV7().String()}
	expect("read signed out", call(t, server, "POST", "/v1/conversations/"+c.ID+"/read", read), 401, "unauthenticated")
	expect("read a bad id", call(t, server, "POST", "/v1/conversations/"+c.ID+"/read", map[string]any{"message_id": "x"}, bearer(buyer.AccessToken)), 400, "")
	expect("read an unknown message", call(t, server, "POST", "/v1/conversations/"+c.ID+"/read", read, bearer(buyer.AccessToken)), 404, "conversation_not_found")
	expect("unread signed out", call(t, server, "GET", "/v1/me/unread", nil), 401, "unauthenticated")

	if res := call(t, server, "POST", "/v1/blocks", map[string]any{"account_id": buyer.Account.ID}, bearer(seller.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("block: %d %s", res.Status, res.Body)
	}
	expect("send while blocked", send(buyer.AccessToken, c.ID, hi), 403, "blocked")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	expect("open suspended", open(suspended.AccessToken, msg), 403, "account_suspended")
	hidden := activeListing(t, seller.AccessToken)
	if res := call(t, server, "POST", "/v1/listings/"+hidden.ID+"/hide", nil, bearer(seller.AccessToken)); res.Status != http.StatusOK {
		t.Fatalf("hide: %d", res.Status)
	}
	expect("open a hidden listing", open(stranger.AccessToken, map[string]any{"listing_id": hidden.ID, "text": "Hi"}), 409, "listing_unavailable")
}
