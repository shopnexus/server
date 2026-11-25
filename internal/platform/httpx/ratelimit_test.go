package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/getkin/kin-openapi/openapi3"
)

// limitsFor builds Limits from a spec of op, whose x-rate-limit is tier
// when not "", at POST /v1/op.
func limitsFor(t *testing.T, tier string, now *time.Time) *Limits {
	t.Helper()
	op := &openapi3.Operation{Responses: openapi3.NewResponses()}
	if tier != "" {
		op.Extensions = map[string]any{"x-rate-limit": tier}
	}
	doc := &openapi3.T{Paths: openapi3.NewPaths(openapi3.WithPath("/v1/op", &openapi3.PathItem{Post: op}))}
	l, err := NewLimits(doc, func() time.Time { return *now }, discard())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func samePath(_, path string) string { return path }

func TestLimitsTierOf(t *testing.T) {
	t.Parallel()
	now := time.Now()
	l := limitsFor(t, "code", &now)
	for _, c := range []struct {
		method, pattern string
		want            Tier
	}{
		{"POST", "/v1/op", Code},
		{"GET", "/v1/op", Read},
		{"DELETE", "/v1/other", Write},
		{"GET", "", Read},
	} {
		if got := l.TierOf(c.method, c.pattern); got != c.want {
			t.Errorf("TierOf(%s %s) = %s, want %s", c.method, c.pattern, got.Name, c.want.Name)
		}
	}
	doc := &openapi3.T{Paths: openapi3.NewPaths(openapi3.WithPath("/v1/op", &openapi3.PathItem{
		Get: &openapi3.Operation{Extensions: map[string]any{"x-rate-limit": "fast"}},
	}))}
	if _, err := NewLimits(doc, time.Now, discard()); err == nil {
		t.Fatal("an unknown tier is accepted")
	}
}

func TestLimitsPerAddress(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	l := limitsFor(t, "code", &now)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	h := ResolveClientIP(nil)(l.Middleware(samePath)(ok))
	from := func(addr string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/op", http.NoBody)
		req.RemoteAddr = netip.AddrPortFrom(netip.MustParseAddr(addr), 5000).String()
		return record(h, req)
	}

	for i := range Code.Address.Burst {
		if got := from("203.0.113.7").Code; got != 202 {
			t.Fatalf("request %d: %d", i, got)
		}
	}
	rec := from("203.0.113.7")
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				RetryAfter int `json:"retry_after"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != 429 || body.Error.Code != "rate_limited" || body.Error.Details.RetryAfter != 10 {
		t.Fatalf("past the burst: %d %s: %v", rec.Code, rec.Body, err)
	}
	if got := from("198.51.100.1").Code; got != 202 {
		t.Fatalf("another address has its own bucket: %d", got)
	}

	// One /64 is one address.
	for range Code.Address.Burst {
		from("2001:db8:1:2::1")
	}
	if got := from("2001:db8:1:2:ffff::9").Code; got != 429 {
		t.Fatalf("another address in the /64: %d", got)
	}
	if got := from("2001:db8:1:3::1").Code; got != 202 {
		t.Fatalf("the next /64: %d", got)
	}

	now = now.Add(10 * time.Second)
	if got := from("203.0.113.7").Code; got != 202 {
		t.Fatalf("a token comes back every 10 s: %d", got)
	}
}

func TestLimitCaller(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	l := limitsFor(t, "auth", &now)
	me, other := uuid.NewV7(), uuid.NewV7()
	var got error
	h := ResolveClientIP(nil)(l.Middleware(samePath)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = LimitCaller(r, uuid.MustParse(r.Header.Get("Caller")))
	})))
	as := func(caller uuid.UUID, addr string) error {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/op", http.NoBody)
		req.RemoteAddr = addr + ":5000"
		req.Header.Set("Caller", caller.String())
		record(h, req)
		return got
	}

	// Each request from its own address, so only the caller's bucket fills.
	for i := range Auth.Caller.Burst {
		if err := as(me, fmt.Sprintf("203.0.113.%d", i+1)); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if err := as(me, "198.51.100.1"); err == nil {
		t.Fatal("the caller past the burst is let through")
	}
	if err := as(other, "198.51.100.2"); err != nil {
		t.Fatalf("another caller has its own bucket: %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/op", http.NoBody)
	if err := LimitCaller(req, me); err != nil {
		t.Fatalf("outside the middleware: %v", err)
	}
}

func TestLimitCountsWhatItWraps(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	l := limitsFor(t, "exempt", &now)
	own := Tier{Name: "own", Address: Rate{1, 3}}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	h := ResolveClientIP(nil)(l.Middleware(samePath)(Limit(own)(ok)))
	codes := map[int]int{}
	for range own.Address.Burst + 1 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/op", http.NoBody)
		req.RemoteAddr = "203.0.113.7:5000"
		codes[record(h, req).Code]++
	}
	if codes[202] != own.Address.Burst || codes[429] != 1 {
		t.Fatalf("codes %v", codes)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Limit takes a shared tier's name")
		}
	}()
	Limit(Tier{Name: Write.Name, Address: Rate{1, 1}})
}

func TestLimitsDropFullBuckets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	l := limitsFor(t, "", &now)
	a := bucketKey{tier: "write", addr: netip.MustParseAddr("203.0.113.7")}
	b := bucketKey{tier: "write", addr: netip.MustParseAddr("198.51.100.1")}
	l.take(a, Write.Address)
	// Write refills in 20 s; the sweep runs once a minute.
	now = now.Add(2 * time.Minute)
	l.take(b, Write.Address)
	if _, kept := l.buckets[a]; kept || len(l.buckets) != 1 {
		t.Fatalf("buckets %v", l.buckets)
	}
}
