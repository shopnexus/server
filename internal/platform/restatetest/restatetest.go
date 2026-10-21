// Package restatetest connects tests to the test stack's Restate server.
package restatetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// URLsForMain returns the test stack's ingress and admin URLs.
func URLsForMain() (string, string, error) {
	ingressURL, adminURL := os.Getenv("TEST_RESTATE_INGRESS_URL"), os.Getenv("TEST_RESTATE_ADMIN_URL")
	if ingressURL == "" || adminURL == "" {
		return "", "", errors.New("TEST_RESTATE_INGRESS_URL and TEST_RESTATE_ADMIN_URL are not set: run `make test-up` and use make")
	}
	return ingressURL, adminURL, nil
}

// URLs skips the test under -short and fails it when the test stack's URLs
// are missing.
func URLs(t testing.TB) (string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Restate")
	}
	ingressURL, adminURL, err := URLsForMain()
	if err != nil {
		t.Fatal(err)
	}
	return ingressURL, adminURL
}

// ConfigForMain returns the test stack's Restate config with a fresh service
// prefix and a free local port for the endpoint.
func ConfigForMain() (config.RestateConfig, error) {
	ingressURL, adminURL, err := URLsForMain()
	return config.RestateConfig{IngressURL: ingressURL, AdminURL: adminURL, ListenAddr: "127.0.0.1:0",
		ServicePrefix: Prefix()}, err
}

// Config is ConfigForMain for one test; it skips under -short like URLs.
func Config(t testing.TB) config.RestateConfig {
	t.Helper()
	URLs(t)
	cfg, err := ConfigForMain()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
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
	ingressURL, adminURL := URLs(t)
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
