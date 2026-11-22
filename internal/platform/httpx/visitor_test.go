package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/telemetry"
)

// visitLog serves req through the API's chain with a handler that runs
// signIn (unless nil), as the auth middleware would, and returns the access
// log line.
func visitLog(t *testing.T, req *http.Request, trusted []netip.Prefix, signIn func(ctx context.Context)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, "json", slog.LevelInfo)
	h := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if signIn != nil {
			signIn(r.Context())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	day := func() time.Time { return time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC) }
	serve(Trace(log)(ResolveClientIP(trusted)(Visitors(nil, trusted, day)(h))), req)
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("log %q: %v", buf.String(), err)
	}
	return m
}

func newReq(t *testing.T, path, ua, remote string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
	req.Header.Set("User-Agent", ua)
	req.RemoteAddr = remote
	return req
}

func TestVisitorsGuest(t *testing.T) {
	t.Parallel()
	m := visitLog(t, newReq(t, "/v1/listings", "okhttp/5.5.1", "14.161.1.1:5000"), nil, nil)
	if m["client"] != "android" {
		t.Fatalf("client %v", m["client"])
	}
	v, ok := m["visitor"].(string)
	if !ok || len(v) != 14 || v[:2] != "g:" {
		t.Fatalf("visitor %q", v)
	}
	// The same address on the same day is the same guest.
	if again := visitLog(t, newReq(t, "/v1/x", "node", "14.161.1.1:6000"), nil, nil); again["visitor"] != v {
		t.Fatalf("visitor %v, then %v", v, again["visitor"])
	}
	if _, ok := m["country"]; !ok {
		t.Fatal("no country key")
	}
}

func TestVisitorsSignedIn(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	m := visitLog(t, newReq(t, "/v1/me", "node", "14.161.1.1:5000"), nil, func(ctx context.Context) {
		authn.With(ctx, authn.Principal{AccountID: id})
	})
	if m["visitor"] != "a:"+id.String() || m["client"] != "website" {
		t.Fatalf("got %v", m)
	}

	mid := uuid.New()
	m = visitLog(t, newReq(t, "/v1/console/accounts", "Mozilla/5.0", "14.161.1.1:5000"), nil, func(ctx context.Context) {
		authn.WithConsoleMember(ctx, authn.ConsoleMember{MemberID: mid})
	})
	if m["visitor"] != "m:"+mid.String() || m["client"] != "console" {
		t.Fatalf("got %v", m)
	}
}

// The website rendering without a buyer, and health probes, are nobody.
func TestVisitorsSkipsProxiesAndProbes(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.42.0.0/16")}
	if m := visitLog(t, newReq(t, "/v1/listings", "node", "10.42.0.7:5000"), trusted, nil); m["visitor"] != nil {
		t.Fatalf("proxy got visitor %v", m["visitor"])
	}
	if m := visitLog(t, newReq(t, "/healthz", "kube-probe/1.34", "10.0.0.1:5000"), nil, nil); m["visitor"] != nil || m["client"] != "other" {
		t.Fatalf("probe got %v", m)
	}
	// Forwarded by a trusted proxy, the buyer behind it counts.
	req := newReq(t, "/v1/listings", "node", "10.42.0.7:5000")
	req.Header.Set("X-Forwarded-For", "113.161.0.1, 10.42.0.9")
	if m := visitLog(t, req, trusted, nil); m["visitor"] == nil {
		t.Fatal("forwarded buyer got no visitor")
	}
}
