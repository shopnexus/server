package e2e

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/provider/bifrost"
	"github.com/shopnexus/server/internal/provider/bifrost/bifrosttest"
)

// serverCallers are the virtual keys the server's code calls with, which
// the reconciler makes at start.
var serverCallers = []string{"server-search", "server-embed-index", "server-embed-search", "server-assist", "server-trust"}

// newGateway is Bifrost for one e2e server: the stack's two providers and
// their catalogs, each server caller's calls going to the stub that plays
// its model.
func newGateway() *bifrosttest.Fake {
	f := bifrosttest.New()
	f.SetProviders(
		bifrost.Provider{Name: "openrouter", BaseURL: "https://openrouter.ai/api", Status: "active"},
		bifrost.Provider{Name: "upstream", BaseURL: "http://upstream.e2e.test/v1", Status: "active", Custom: "openai"},
	)
	f.SetCatalog("openrouter",
		bifrost.Model{Name: "google/gemini-2.5-flash", Provider: "openrouter", Input: new(3e-7), Output: new(2.5e-6)},
		bifrost.Model{Name: "baai/bge-m3", Provider: "openrouter", Input: new(1e-8), Output: new(0.0)},
		bifrost.Model{Name: "qwen/qwen3-embedding-8b", Provider: "openrouter", Input: new(1e-8), Output: new(0.0)},
	)
	f.SetCatalog("upstream", bifrost.Model{Name: "claude-haiku-5-5", Provider: "upstream"},
		bifrost.Model{Name: "claude-sonnet-5-5", Provider: "upstream"})
	f.SetUpstream("server-search", model.url())
	f.SetUpstream("server-embed-index", embedder.url())
	f.SetUpstream("server-embed-search", embedder.url())
	f.SetUpstream("server-assist", assistStub.url())
	f.SetUpstream("server-trust", scamStub.url())
	return f
}

// ownGateway gives a second server a Bifrost of its own, so its reconciler,
// on a database of its own, leaves the shared one's keys alone.
func ownGateway(t *testing.T) string {
	t.Helper()
	f := newGateway()
	t.Cleanup(f.Close)
	return f.URL()
}

// gatewayReady waits for the shared server's first reconcile to make the
// server callers' keys, which it runs in the background at start.
func gatewayReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		keys := gateway.Keys()
		if !slices.ContainsFunc(serverCallers, func(c string) bool { _, ok := keys[c]; return !ok }) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("the server made no Bifrost keys in 30 s")
		case <-tick.C:
		}
	}
}
