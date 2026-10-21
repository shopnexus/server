// Package restatetest connects tests to the test stack's Restate server.
package restatetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// URLs returns TEST_RESTATE_INGRESS_URL and TEST_RESTATE_ADMIN_URL, or the
// test stack's when they are unset.
func URLs() (string, string) {
	return getenv("TEST_RESTATE_INGRESS_URL", "http://127.0.0.1:56581"), getenv("TEST_RESTATE_ADMIN_URL", "http://127.0.0.1:56571")
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ConfigForMain returns the test stack's Restate config with a fresh service
// prefix and a free local port for the endpoint.
func ConfigForMain() config.RestateConfig {
	ingressURL, adminURL := URLs()
	return config.RestateConfig{IngressURL: ingressURL, AdminURL: adminURL, ListenAddr: "127.0.0.1:0",
		ServicePrefix: Prefix()}
}

// Config is ConfigForMain for one test, which it skips under -short.
func Config(t testing.TB) config.RestateConfig {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Restate")
	}
	return ConfigForMain()
}

// Prefix returns a fresh service-name prefix. Test packages run in parallel
// against one Restate server, and a service name routes to one endpoint, so
// every registration needs names of its own.
func Prefix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "t" + hex.EncodeToString(b) + "_"
}

// Start serves defs on a free local port, registers them with the test
// stack's Restate and returns an ingress client.
func Start(t testing.TB, defs ...restate.ServiceDefinition) *ingress.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Restate")
	}
	ingressURL, adminURL := URLs()
	ep, err := restatex.Serve("127.0.0.1:0", defs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ep.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := restatex.Register(ctx, adminURL, ep.URL()); err != nil {
		t.Fatal(err)
	}
	return ingress.NewClient(ingressURL)
}
