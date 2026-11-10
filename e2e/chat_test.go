package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/testenv"
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

// openChat has token open a conversation from listing with text.
func openChat(t *testing.T, token, listing, text string) conversationBody {
	t.Helper()
	res := call(t, "POST", "/v1/conversations", map[string]any{"listing_id": listing, "text": text}, bearer(token), newKey())
	return expect[conversationBody](t, res, http.StatusCreated)
}

func sendChat(t *testing.T, token, id string, body map[string]any) messageBody {
	t.Helper()
	res := call(t, "POST", "/v1/conversations/"+id+"/messages", body, bearer(token), newKey())
	return expect[messageBody](t, res, http.StatusCreated)
}

func chatMessages(t *testing.T, token, id string) []messageBody {
	t.Helper()
	return expect[page[messageBody]](t, call(t, "GET", "/v1/conversations/"+id+"/messages", nil, bearer(token)), http.StatusOK).Items
}

func TestChat(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	sock := openSocket(t, seller.AccessToken, "")
	sock.ready(t)

	c := openChat(t, buyer.AccessToken, l.ID, "Còn không bạn?")
	if c.Other.ID != seller.Account.ID {
		t.Fatalf("%+v", c)
	}
	sock.waitFrame(t, "update", "message", c.ID)

	reply := sendChat(t, seller.AccessToken, c.ID, map[string]any{"text": "Còn bạn ơi"})
	if inbox := inboxView(t, buyer.AccessToken, ""); len(inbox.Items) != 1 || inbox.Items[0].Unread != 1 || inbox.Unread != 1 {
		t.Fatalf("inbox: %+v", inbox)
	}
	unread := expect[map[string]int](t, call(t, "GET", "/v1/me/unread", nil, bearer(buyer.AccessToken)), http.StatusOK)
	if unread["unread"] != 1 {
		t.Fatalf("unread: %v", unread)
	}
	expectStatus(t, call(t, "POST", "/v1/conversations/"+c.ID+"/read", map[string]any{"message_id": reply.ID}, bearer(buyer.AccessToken)), http.StatusNoContent)
	if unread = decode[map[string]int](t, call(t, "GET", "/v1/me/unread", nil, bearer(buyer.AccessToken))); unread["unread"] != 0 {
		t.Fatalf("unread after reading: %v", unread)
	}

	photo := sendChat(t, buyer.AccessToken, c.ID, map[string]any{"text": "Vết này", "photos": []string{upload(t, buyer.AccessToken, photoWithGPS(t))}})
	var last messageBody // the newest message the seller reads
	testenv.Eventually(t, 30*time.Second, func() error {
		if last = chatMessages(t, seller.AccessToken, c.ID)[0]; last.ID != photo.ID {
			return fmt.Errorf("the seller's newest message is %s, want %s", last.ID, photo.ID)
		}
		return nil
	})
	if last.Status != "sent" || last.Photos[0].URL == "" {
		t.Fatalf("%+v", last)
	}

	expectStatus(t, call(t, "POST", "/v1/conversations/"+c.ID+"/archive", nil, bearer(seller.AccessToken)), http.StatusNoContent)
	if archived := inboxView(t, seller.AccessToken, "archived=true"); len(archived.Items) != 1 || !archived.Items[0].Archived {
		t.Fatalf("archived: %+v", archived)
	}
	expectStatus(t, call(t, "POST", "/v1/conversations/"+c.ID+"/unarchive", nil, bearer(seller.AccessToken)), http.StatusNoContent)

	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	got := expect[chatScreen](t, call(t, "GET", "/v1/screens/chat/"+c.ID, nil, bearer(seller.AccessToken)), http.StatusOK)
	if len(got.Deals) != 1 || got.Deals[0].ID != d.ID {
		t.Fatalf("chat: %+v", got)
	}
}

func TestChatRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	c := openChat(t, buyer.AccessToken, l.ID, "Còn không?")
	open := func(token string, body any) Response {
		t.Helper()
		return call(t, "POST", "/v1/conversations", body, newKey(), bearer(token))
	}
	msg := map[string]any{"listing_id": l.ID, "text": "Còn không?"}

	check(t, "open signed out", open("", msg), 401, "unauthenticated")
	check(t, "open a bad id", open(buyer.AccessToken, map[string]any{"listing_id": "x"}), 400, "")
	check(t, "open an unknown listing", open(buyer.AccessToken, map[string]any{"listing_id": uuid.NewV7().String(), "text": "Hi"}), 404, "listing_not_found")
	check(t, "open own listing", open(seller.AccessToken, msg), 409, "own_listing")
	check(t, "open without text", open(stranger.AccessToken, map[string]any{"listing_id": l.ID, "text": " "}), 422, "invalid_message")
	check(t, "open with another's photo", open(stranger.AccessToken, map[string]any{"listing_id": l.ID, "photos": []string{upload(t, seller.AccessToken, photoWithGPS(t))}}), 422, "invalid_upload")

	check(t, "messages signed out", call(t, "GET", "/v1/conversations/"+c.ID+"/messages", nil), 401, "unauthenticated")
	check(t, "messages a bad id", call(t, "GET", "/v1/conversations/x/messages", nil, bearer(buyer.AccessToken)), 400, "")
	check(t, "messages as a stranger", call(t, "GET", "/v1/conversations/"+c.ID+"/messages", nil, bearer(stranger.AccessToken)), 404, "conversation_not_found")

	send := func(token, id string, body any) Response {
		t.Helper()
		return call(t, "POST", "/v1/conversations/"+id+"/messages", body, newKey(), bearer(token))
	}
	hi := map[string]any{"text": "Hi"}
	check(t, "send signed out", send("", c.ID, hi), 401, "unauthenticated")
	check(t, "send a bad id", send(buyer.AccessToken, "x", hi), 400, "")
	check(t, "send as a stranger", send(stranger.AccessToken, c.ID, hi), 404, "conversation_not_found")
	check(t, "send nothing", send(buyer.AccessToken, c.ID, map[string]any{"text": ""}), 422, "invalid_message")

	for _, action := range []string{"archive", "unarchive"} {
		check(t, action+" signed out", call(t, "POST", "/v1/conversations/"+c.ID+"/"+action, nil), 401, "unauthenticated")
		check(t, action+" a bad id", call(t, "POST", "/v1/conversations/x/"+action, nil, bearer(buyer.AccessToken)), 400, "")
		check(t, action+" as a stranger", call(t, "POST", "/v1/conversations/"+c.ID+"/"+action, nil, bearer(stranger.AccessToken)), 404, "conversation_not_found")
	}
	read := map[string]any{"message_id": uuid.NewV7().String()}
	check(t, "read signed out", call(t, "POST", "/v1/conversations/"+c.ID+"/read", read), 401, "unauthenticated")
	check(t, "read a bad id", call(t, "POST", "/v1/conversations/"+c.ID+"/read", map[string]any{"message_id": "x"}, bearer(buyer.AccessToken)), 400, "")
	check(t, "read an unknown message", call(t, "POST", "/v1/conversations/"+c.ID+"/read", read, bearer(buyer.AccessToken)), 404, "conversation_not_found")
	check(t, "unread signed out", call(t, "GET", "/v1/me/unread", nil), 401, "unauthenticated")

	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": buyer.Account.ID}, bearer(seller.AccessToken)), http.StatusNoContent)
	check(t, "send while blocked", send(buyer.AccessToken, c.ID, hi), 403, "blocked")
	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "open suspended", open(suspended.AccessToken, msg), 403, "account_suspended")
	hidden := activeHidden(t, seller.AccessToken)
	check(t, "open a hidden listing", open(stranger.AccessToken, map[string]any{"listing_id": hidden, "text": "Hi"}), 409, "listing_unavailable")
}

func TestMarkConversationUnread(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	c := openChat(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, "Còn không bạn?")
	path := "/v1/conversations/" + c.ID + "/unread"
	check(t, "mark", call(t, "POST", path, nil, bearer(buyer.AccessToken)), 204, "")
	if got := expect[map[string]int](t, call(t, "GET", "/v1/me/unread", nil, bearer(buyer.AccessToken)), http.StatusOK); got["unread"] != 1 {
		t.Fatalf("unread after the mark: %v", got)
	}
	last := chatMessages(t, buyer.AccessToken, c.ID)[0]
	expectStatus(t, call(t, "POST", "/v1/conversations/"+c.ID+"/read", map[string]any{"message_id": last.ID}, bearer(buyer.AccessToken)), http.StatusNoContent)
	if got := expect[map[string]int](t, call(t, "GET", "/v1/me/unread", nil, bearer(buyer.AccessToken)), http.StatusOK); got["unread"] != 0 {
		t.Fatalf("unread after reading: %v", got)
	}
	check(t, "mark signed out", call(t, "POST", path, nil), 401, "unauthenticated")
	check(t, "mark a bad id", call(t, "POST", "/v1/conversations/x/unread", nil, bearer(buyer.AccessToken)), 400, "")
	stranger, _ := newAccount(t)
	check(t, "mark someone else's", call(t, "POST", path, nil, bearer(stranger.AccessToken)), 404, "conversation_not_found")
}
