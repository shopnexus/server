package e2e

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

// streamEvent is one server-sent event.
type streamEvent struct {
	Name string
	Data string
}

// openStream opens /v1/stream as token, records its status for the
// coverage check, and returns its events until the test ends.
func openStream(t *testing.T, token string) <-chan streamEvent {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server+"/v1/stream", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	record("openStream", res.StatusCode)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	events := make(chan streamEvent, 64)
	go func() {
		defer close(events)
		var ev streamEvent
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			switch line := sc.Text(); {
			case strings.HasPrefix(line, "event: "):
				ev.Name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.Data = strings.TrimPrefix(line, "data: ")
			case line == "" && ev.Name != "":
				events <- ev
				ev = streamEvent{}
			default:
				// A comment, or a blank line ending no event.
			}
		}
	}()
	if ev := nextEvent(t, events); ev.Name != "ready" {
		t.Fatalf("first event %+v", ev)
	}
	return events
}

// nextEvent waits up to 5 s for the next event.
func nextEvent(t *testing.T, events <-chan streamEvent) streamEvent {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("the stream ended")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event within 5s")
	}
	return streamEvent{}
}

// waitEvent reads events for up to 5 s until one named name whose data
// contains id.
func waitEvent(t *testing.T, events <-chan streamEvent, name, id string) {
	t.Helper()
	testenv.Eventually(t, 5*time.Second, func() error {
		if ev := nextEvent(t, events); ev.Name != name || !strings.Contains(ev.Data, id) {
			return fmt.Errorf("event %s %s, want %s for %s", ev.Name, ev.Data, name, id)
		}
		return nil
	})
}

func TestStreamGetsADealHint(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	events := openStream(t, seller.AccessToken)
	d := openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
	moved(t, buyer.AccessToken, d.ID, "offer", map[string]any{"amount": 1_000_000})
	waitEvent(t, events, "deal", d.ID)
	expectStatus(t, call(t, "GET", "/v1/stream", nil), http.StatusUnauthorized)
}
