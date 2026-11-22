package e2e

import (
	"testing"
	"uuid"
)

func TestClientEvents(t *testing.T) {
	t.Parallel()
	send := func(body any) Response {
		t.Helper()
		return call(t, "POST", "/v1/client-events", body)
	}
	search := uuid.NewV7().String()
	events := []map[string]any{
		{"type": "error", "message": "x is undefined", "page": "/search"},
		{"type": "vital", "name": "INP", "value": 180, "page": "/listings/[slug]"},
		{"type": "request", "method": "GET", "path": "/v1/search", "status": 200, "duration_ms": 640, "network": "cellular"},
		{"type": "search_impression", "search_id": search, "listing_ids": []string{uuid.NewV7().String()}, "offset": 0},
		{"type": "search_click", "search_id": search, "listing_id": uuid.NewV7().String(), "position": 0},
	}
	check(t, "send every type", send(map[string]any{"app": "website", "events": events}), 202, "")
	check(t, "an unknown type", send(map[string]any{"app": "android", "events": []map[string]any{{"type": "tap"}}}), 400, "")
	check(t, "a field of another type", send(map[string]any{"app": "android", "events": []map[string]any{
		{"type": "vital", "name": "LCP", "value": 1, "listing_id": uuid.NewV7().String()},
	}}), 400, "")
	check(t, "no events", send(map[string]any{"app": "website", "events": []any{}}), 400, "")

	// One address gets a burst of 60 batches; the e2e calls all come from one.
	one := map[string]any{"app": "android", "events": events[:1]}
	for i := range 100 {
		if res := send(one); res.Status == 429 {
			check(t, "past the burst", res, 429, "rate_limited")
			return
		}
		if i == 99 {
			t.Fatal("100 batches in a row were not rate limited")
		}
	}
}
