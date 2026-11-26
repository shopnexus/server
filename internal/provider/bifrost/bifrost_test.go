package bifrost_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/provider/bifrost"
	"github.com/shopnexus/server/internal/provider/bifrost/bifrosttest"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/llm"
)

// upstream answers chat with {"ok":true} and embeddings with [1, 0].
func upstream(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// serverKey makes caller's key as the reconciler writes it.
func serverKey(t *testing.T, f *bifrosttest.Fake, caller string, meta map[string]string, models map[string][]string) bifrost.VirtualKey {
	t.Helper()
	ctx := context.Background()
	a := f.Admin()
	team, ok := f.Teams()[caller+"-team"]
	if !ok {
		var err error
		if team, err = a.CreateTeam(ctx, caller+"-team", new(30.0)); err != nil {
			t.Fatal(err)
		}
	}
	full := map[string]string{bifrost.MetaOwner: bifrost.OwnerServer}
	maps.Copy(full, meta)
	k, err := a.CreateVirtualKey(ctx, bifrost.KeyInput{Name: caller, TeamID: team.ID, Models: models, Active: true, Metadata: full})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRingAndChat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bifrosttest.New()
	t.Cleanup(f.Close)
	f.SetUpstream("server-search", upstream(t))
	serverKey(t, f, "server-search", map[string]string{
		bifrost.MetaModel: "openrouter/google/gemini-2.5-flash", bifrost.MetaFallbacks: "upstream/claude-haiku-5-5",
		bifrost.MetaExtraBody: `{"provider":{"order":["DeepInfra"]}}`,
	}, map[string][]string{"openrouter": {"google/gemini-2.5-flash"}, "upstream": {"claude-haiku-5-5"}})
	// A key that is not the server's stays out of the ring.
	f.PutKey(bifrost.VirtualKey{Name: "someone", Value: "sk-bf-someone", Metadata: map[string]string{}})
	ring := bifrost.NewRing(f.Admin(), time.Now)
	chat := bifrost.Chat{BaseURL: f.URL(), Name: "server-search", Ring: ring, HTTP: http.DefaultClient, Effort: "low"}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := chat.Complete(ctx, llm.Request{System: "s", User: "u"}, &out); err != nil || !out.OK {
		t.Fatalf("Complete = %v, %+v", err, out)
	}
	calls := f.Calls("server-search")
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	fallbacks, _ := json.Marshal(c.Body["fallbacks"])
	if c.Body["model"] != "openrouter/google/gemini-2.5-flash" || string(fallbacks) != `["upstream/claude-haiku-5-5"]` ||
		c.Body["provider"] == nil || c.Body["reasoning_effort"] != "low" || c.Header.Get("X-Bf-Passthrough-Extra-Params") != "true" {
		t.Fatalf("call = %+v", c)
	}
	if _, err := ring.Route(ctx, "someone"); !errors.Is(err, bifrost.ErrNoKey) {
		t.Fatalf("Route(someone) = %v, want ErrNoKey", err)
	}
}

// A key the reconciler made again has a new value: the 401 reads the ring
// again and the call goes through once more.
func TestCallerRetriesANewKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bifrosttest.New()
	t.Cleanup(f.Close)
	f.SetUpstream("server-embed-index", upstream(t))
	models := map[string][]string{"openrouter": {"baai/bge-m3"}}
	meta := map[string]string{bifrost.MetaModel: "openrouter/baai/bge-m3"}
	old := serverKey(t, f, "server-embed-index", meta, models)
	ring := bifrost.NewRing(f.Admin(), time.Now)
	if _, err := ring.Route(ctx, "server-embed-index"); err != nil {
		t.Fatal(err)
	}
	if err := f.Admin().DeleteVirtualKey(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	now := serverKey(t, f, "server-embed-index", meta, models)
	e := bifrost.Embedder{BaseURL: f.URL(), Name: "server-embed-index", Ring: ring, HTTP: http.DefaultClient, Dims: 2}
	vs, err := e.Embed(ctx, []string{"x"})
	if err != nil || len(vs) != 1 {
		t.Fatalf("Embed = %v, %v", vs, err)
	}
	calls := f.Calls("server-embed-index")
	if len(calls) != 1 || calls[0].Header.Get("Authorization") != "Bearer "+now.Value || calls[0].Header.Get("X-Bf-Passthrough-Extra-Params") != "" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestAdminErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bifrosttest.New()
	t.Cleanup(f.Close)
	a := f.Admin()
	if _, err := a.CreateTeam(ctx, "t", new(-1.0)); !errors.Is(err, bifrost.ErrRefused) {
		t.Fatalf("a budget under 0 = %v, want ErrRefused", err)
	}
	// A wrong password is the server's fault, not the request's.
	a.Password = "wrong"
	if _, err := a.Teams(ctx); err == nil || errors.Is(err, bifrost.ErrRefused) {
		t.Fatalf("a wrong password = %v, want an error other than ErrRefused", err)
	}
	f.SetDown(true)
	if _, err := f.Admin().Teams(ctx); err == nil || errors.Is(err, bifrost.ErrRefused) {
		t.Fatalf("down = %v, want an error other than ErrRefused", err)
	}
	// The ring keeps serving what it read while Bifrost is down.
	f.SetDown(false)
	serverKey(t, f, "server-trust", map[string]string{bifrost.MetaModel: "upstream/claude-haiku-5-5"},
		map[string][]string{"upstream": {"claude-haiku-5-5"}})
	clock := time.Now()
	ring := bifrost.NewRing(f.Admin(), func() time.Time { return clock })
	if _, err := ring.Route(ctx, "server-trust"); err != nil {
		t.Fatal(err)
	}
	f.SetDown(true)
	clock = clock.Add(time.Hour)
	if r, err := ring.Route(ctx, "server-trust"); err != nil || r.Model != "upstream/claude-haiku-5-5" {
		t.Fatalf("stale Route = %+v, %v", r, err)
	}
}

func TestPricePatch(t *testing.T) {
	t.Parallel()
	got := bifrost.PricePatch(bifrost.Price{Input: 1e-6, Output: 5e-6, CacheRead: new(1e-7)})
	want := map[string]float64{
		"input_cost_per_token": 1e-6, "output_cost_per_token": 5e-6,
		"cache_read_input_token_cost": 1e-7, "cache_creation_input_token_cost": 1e-6,
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("PricePatch = %v, want %v", got, want)
		}
	}
	if !slices.Equal(bifrost.SplitList(bifrost.JoinList([]string{"a/b", "c/d"})), []string{"a/b", "c/d"}) {
		t.Fatal("JoinList and SplitList disagree")
	}
}

// counted is f's admin with a count of the virtual-key reads.
func counted(f *bifrosttest.Fake) (bifrost.Admin, *atomic.Int32) {
	n := &atomic.Int32{}
	a := f.Admin()
	a.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "virtual-keys") {
			n.Add(1)
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	return a, n
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// clock is a test clock goroutines may read.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }

func (c *clock) add(d time.Duration) { c.mu.Lock(); c.at = c.at.Add(d); c.mu.Unlock() }

// A failed read is not tried again for a few seconds, and a cancelled
// wait ends at once.
func TestRingBacksOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bifrosttest.New()
	t.Cleanup(f.Close)
	serverKey(t, f, "server-trust", map[string]string{bifrost.MetaModel: "upstream/claude-haiku-5-5"},
		map[string][]string{"upstream": {"claude-haiku-5-5"}})
	a, reads := counted(f)
	c := &clock{at: time.Now()}
	ring := bifrost.NewRing(a, c.now)
	f.SetDown(true)
	for range 3 {
		if _, err := ring.Route(ctx, "server-trust"); err == nil || errors.Is(err, bifrost.ErrNoKey) {
			t.Fatalf("Route while down = %v, want the read's error", err)
		}
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1", n)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ring.Route(cancelled, "server-trust"); err == nil || reads.Load() != 1 {
		t.Fatalf("cancelled Route = %v, reads = %d", err, reads.Load())
	}
	f.SetDown(false)
	c.add(time.Minute)
	if r, err := ring.Route(ctx, "server-trust"); err != nil || r.Model != "upstream/claude-haiku-5-5" {
		t.Fatalf("Route after the backoff = %+v, %v", r, err)
	}
}

// An upstream 401 passed through is the provider's key, not the server's:
// the ring is not read again.
func TestUpstreamRefusalKeepsTheRing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bifrosttest.New()
	t.Cleanup(f.Close)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad provider key"},"type":"provider_auth_failed"}`))
	}))
	t.Cleanup(up.Close)
	f.SetUpstream("server-search", up.URL)
	serverKey(t, f, "server-search", map[string]string{bifrost.MetaModel: "openrouter/google/gemini-2.5-flash"},
		map[string][]string{"openrouter": {"google/gemini-2.5-flash"}})
	a, reads := counted(f)
	ring := bifrost.NewRing(a, time.Now)
	chat := bifrost.Chat{BaseURL: f.URL(), Name: "server-search", Ring: ring, HTTP: http.DefaultClient}
	var out struct{}
	if err := chat.Complete(ctx, llm.Request{System: "s", User: "u"}, &out); err == nil {
		t.Fatal("Complete = nil, want the upstream's 401")
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("reads = %d, want 1", n)
	}
}

// A team whose budget is spent answers 402, which both clients name.
func TestBudgetSpent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := bifrosttest.New()
	t.Cleanup(f.Close)
	f.SetUpstream("server-embed-index", upstream(t))
	serverKey(t, f, "server-embed-index", map[string]string{bifrost.MetaModel: "openrouter/baai/bge-m3"},
		map[string][]string{"openrouter": {"baai/bge-m3"}})
	f.SetSpent("server-embed-index-team", 30)
	ring := bifrost.NewRing(f.Admin(), time.Now)
	e := bifrost.Embedder{BaseURL: f.URL(), Name: "server-embed-index", Ring: ring, HTTP: http.DefaultClient, Dims: 2}
	if _, err := e.Embed(ctx, []string{"x"}); !errors.Is(err, embed.ErrBudgetSpent) || errors.Is(err, embed.ErrRefused) {
		t.Fatalf("Embed = %v, want ErrBudgetSpent", err)
	}
	chat := bifrost.Chat{BaseURL: f.URL(), Name: "server-embed-index", Ring: ring, HTTP: http.DefaultClient}
	var out struct{}
	if err := chat.Complete(ctx, llm.Request{System: "s", User: "u"}, &out); !errors.Is(err, llm.ErrBudgetSpent) {
		t.Fatalf("Complete = %v, want ErrBudgetSpent", err)
	}
}
