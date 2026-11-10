package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

type socketFrame struct {
	Type           string `json:"type"`
	Seq            int64  `json:"seq"`
	Kind           string `json:"kind"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	DealID         string `json:"deal_id"`
}

type updatePage struct {
	Updates []socketFrame `json:"updates"`
	Seq     int64         `json:"seq"`
	Reset   bool          `json:"reset"`
	HasMore bool          `json:"has_more"`
}

type socket struct {
	conn   *websocket.Conn
	frames chan socketFrame
}

// openSocket opens /v1/ws as token with query, records the status for the
// coverage check, and reads frames until the test ends.
func openSocket(t *testing.T, token, query string) *socket {
	t.Helper()
	conn, res, err := websocket.Dial(t.Context(), server+"/v1/ws"+query, //nolint:bodyclose // Dial closes the handshake response's body.
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	record("openSocket", res.StatusCode)
	s := &socket{conn: conn, frames: make(chan socketFrame, 256)}
	go func() {
		defer close(s.frames)
		for {
			_, b, readErr := conn.Read(t.Context())
			if readErr != nil {
				return
			}
			var f socketFrame
			if json.Unmarshal(b, &f) == nil {
				s.frames <- f
			}
		}
	}()
	t.Cleanup(func() { _ = conn.CloseNow() })
	return s
}

func (s *socket) next(t *testing.T) socketFrame {
	t.Helper()
	select {
	case f, ok := <-s.frames:
		if !ok {
			t.Fatal("the socket closed")
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5s")
	}
	return socketFrame{}
}

// ready reads frames up to ready and returns them, ready last.
func (s *socket) ready(t *testing.T) []socketFrame {
	t.Helper()
	var out []socketFrame
	for {
		f := s.next(t)
		out = append(out, f)
		if f.Type == "ready" {
			return out
		}
	}
}

// waitFrame reads frames for up to 5 s until one of typ and kind naming id.
func (s *socket) waitFrame(t *testing.T, typ, kind, id string) {
	t.Helper()
	testenv.Eventually(t, 5*time.Second, func() error {
		got := s.next(t)
		if got.Type != typ || got.Kind != kind || (got.ConversationID != id && got.DealID != id && got.MessageID != id) {
			return fmt.Errorf("frame %+v, want %s %s for %s", got, typ, kind, id)
		}
		return nil
	})
}

func TestSocketGetsADealUpdate(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	s := openSocket(t, seller.AccessToken, "")
	s.ready(t)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	s.waitFrame(t, "update", "deal", d.ID)
	expectStatus(t, call(t, "GET", "/v1/ws", nil), http.StatusUnauthorized)
}

// The seller misses three messages while offline and gets exactly them,
// in seq order, when the socket comes back with since.
func TestSocketCatchesUpAfterAReconnect(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	first := openSocket(t, seller.AccessToken, "")
	at := first.ready(t)[0].Seq
	_ = first.conn.Close(websocket.StatusNormalClosure, "")

	c := openChat(t, buyer.AccessToken, l.ID, "Còn không bạn?")
	sent := map[string]bool{chatMessages(t, buyer.AccessToken, c.ID)[0].ID: true}
	sent[sendChat(t, buyer.AccessToken, c.ID, map[string]any{"text": "Giá bao nhiêu?"}).ID] = true
	sent[sendChat(t, buyer.AccessToken, c.ID, map[string]any{"text": "Mình lấy nhé"}).ID] = true

	frames := openSocket(t, seller.AccessToken, "?since="+strconv.FormatInt(at, 10)).ready(t)
	got := map[string]bool{}
	want := at + 1
	for _, f := range frames[:len(frames)-1] {
		if f.Type != "update" || f.Seq != want {
			t.Fatalf("frame %+v, want update %d", f, want)
		}
		want++
		if f.Kind == "message" && f.ConversationID == c.ID {
			got[f.MessageID] = true
		}
	}
	if ready := frames[len(frames)-1]; ready.Seq != want-1 {
		t.Fatalf("ready %+v after %d", ready, want-1)
	}
	if len(got) != 3 {
		t.Fatalf("message updates %v, sent %v", got, sent)
	}
	for id := range sent {
		if !got[id] {
			t.Fatalf("message %s missing from %v", id, got)
		}
	}
}

func TestSocketTypingAndActivity(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	c := openChat(t, buyer.AccessToken, l.ID, "Còn không bạn?")
	reader := openSocket(t, buyer.AccessToken, "")
	reader.ready(t)
	typist := openSocket(t, seller.AccessToken, "")
	typist.ready(t)
	b, _ := json.Marshal(map[string]any{"type": "typing", "conversation_id": c.ID})
	if err := typist.conn.Write(t.Context(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, 5*time.Second, func() error {
		if f := reader.next(t); f.Type != "typing" || f.ConversationID != c.ID {
			return fmt.Errorf("frame %+v", f)
		}
		return nil
	})
	testenv.Eventually(t, 5*time.Second, func() error {
		var conv struct {
			Other struct {
				LastActiveAt string `json:"last_active_at"`
			} `json:"other"`
		}
		res := call(t, "GET", "/v1/screens/chat/"+c.ID, nil, bearer(buyer.AccessToken))
		if err := json.Unmarshal(res.Body, &conv); err != nil || conv.Other.LastActiveAt == "" {
			return fmt.Errorf("conversation %s: %w", res.Body, err)
		}
		var screen struct {
			Seller struct {
				LastActiveAt string `json:"last_active_at"`
			} `json:"seller"`
		}
		res = call(t, "GET", "/v1/screens/seller/"+seller.Account.ID, nil, bearer(buyer.AccessToken))
		if err := json.Unmarshal(res.Body, &screen); err != nil || screen.Seller.LastActiveAt == "" {
			return fmt.Errorf("seller screen %s: %w", res.Body, err)
		}
		return nil
	})
}

func TestUpdates(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	c := openChat(t, buyer.AccessToken, l.ID, "Còn không bạn?")
	testenv.Eventually(t, 5*time.Second, func() error {
		p := expect[updatePage](t, call(t, "GET", "/v1/updates?since=0", nil, bearer(seller.AccessToken)), http.StatusOK)
		for _, u := range p.Updates {
			if u.Kind == "message" && u.ConversationID == c.ID {
				return nil
			}
		}
		return fmt.Errorf("no message update in %+v", p)
	})
	expectStatus(t, call(t, "GET", "/v1/updates", nil), http.StatusUnauthorized)
}
