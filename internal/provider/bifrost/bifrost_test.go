package bifrost_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/provider/bifrost"
	"github.com/shopnexus/server/internal/provider/bifrost/bifrosttest"
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
	for k, v := range meta {
		full[k] = v
	}
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
	chat := bifrost.Chat{Caller: bifrost.Caller{BaseURL: f.URL(), Name: "server-search", Ring: ring, HTTP: http.DefaultClient}, Effort: "low"}
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
		c.Body["provider"] == nil || c.Body["reasoning_effort"] != "low" || c.Header.Get("x-bf-passthrough-extra-params") != "true" {
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
	e := bifrost.Embedder{Caller: bifrost.Caller{BaseURL: f.URL(), Name: "server-embed-index", Ring: ring, HTTP: http.DefaultClient}, Dims: 2}
	vs, err := e.Embed(ctx, []string{"x"})
	if err != nil || len(vs) != 1 {
		t.Fatalf("Embed = %v, %v", vs, err)
	}
	calls := f.Calls("server-embed-index")
	if len(calls) != 1 || calls[0].Header.Get("Authorization") != "Bearer "+now.Value || calls[0].Header.Get("x-bf-passthrough-extra-params") != "" {
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
	a.Password = "wrong"
	if _, err := a.Teams(ctx); !errors.Is(err, bifrost.ErrRefused) {
		t.Fatalf("a wrong password = %v, want ErrRefused", err)
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
