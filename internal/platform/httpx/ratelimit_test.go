package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestRateLimitPerAddress(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	h := ResolveClientIP(nil)(RateLimit(1, 2, func() time.Time { return now }, discard())(ok))
	from := func(addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/client-events", http.NoBody)
		req.RemoteAddr = addr + ":5000"
		return serve(h, req)
	}

	for i, want := range []int{202, 202, 429} {
		if got := from("203.0.113.7").Code; got != want {
			t.Fatalf("request %d: %d, want %d", i, got, want)
		}
	}
	if got := from("198.51.100.1").Code; got != 202 {
		t.Fatalf("another address has its own bucket: %d", got)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				RetryAfter int `json:"retry_after"`
			} `json:"details"`
		} `json:"error"`
	}
	rec := from("203.0.113.7")
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "rate_limited" || body.Error.Details.RetryAfter != 1 {
		t.Fatalf("429 body %s: %v", rec.Body, err)
	}

	now = now.Add(time.Second)
	if got := from("203.0.113.7").Code; got != 202 {
		t.Fatalf("a token comes back each second: %d", got)
	}
}

func TestRateLimitDropsIdleAddresses(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	l := &limits{every: 1, burst: 1, seen: map[netip.Addr]*limit{}}
	l.allow(netip.MustParseAddr("203.0.113.7"), now)
	l.allow(netip.MustParseAddr("198.51.100.1"), now.Add(idleAfter+time.Second))
	if _, kept := l.seen[netip.MustParseAddr("203.0.113.7")]; kept || len(l.seen) != 1 {
		t.Fatalf("seen %v", l.seen)
	}
}
