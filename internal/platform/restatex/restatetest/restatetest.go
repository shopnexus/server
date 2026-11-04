// Package restatetest connects tests to the test stack's Restate server.
package restatetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/platform/testenv"
)

// URLs returns TEST_RESTATE_INGRESS_URL and TEST_RESTATE_ADMIN_URL, from the
// environment or .env.test.
func URLs() (ingressURL, adminURL string, err error) {
	if ingressURL, err = testenv.Get("TEST_RESTATE_INGRESS_URL"); err != nil {
		return "", "", fmt.Errorf("test restate: %w", err)
	}
	if adminURL, err = testenv.Get("TEST_RESTATE_ADMIN_URL"); err != nil {
		return "", "", fmt.Errorf("test restate: %w", err)
	}
	return ingressURL, adminURL, nil
}

// identityFile is the key make test writes and mounts into the test stack's
// Restate, which signs every request with it.
func identityFile() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".restate", "identity.pem")
}

// ConfigForMain returns the test stack's Restate config with a fresh service
// prefix, a free local port for the endpoint and the stack's identity key.
func ConfigForMain() (config.RestateConfig, error) {
	key, err := restatex.IdentityKey(identityFile())
	if err != nil {
		return config.RestateConfig{}, fmt.Errorf("restate identity key: %w; make test writes it", err)
	}
	ingressURL, adminURL, err := URLs()
	if err != nil {
		return config.RestateConfig{}, err
	}
	return config.RestateConfig{
		IngressURL: ingressURL, AdminURL: adminURL, ListenAddr: "127.0.0.1:0", EndpointURL: "",
		ServicePrefix: Prefix(), IdentityKey: key, Register: true,
	}, nil
}

// Config is ConfigForMain for one test, which it skips under -short.
func Config(tb testing.TB) config.RestateConfig {
	tb.Helper()
	if testing.Short() {
		tb.Skip("needs Restate")
	}
	cfg, err := ConfigForMain()
	if err != nil {
		tb.Fatal(err)
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
func Start(tb testing.TB, defs ...restate.ServiceDefinition) *ingress.Client {
	tb.Helper()
	cfg := Config(tb)
	ep, err := restatex.Serve(tb.Context(), cfg.ListenAddr, cfg.IdentityKey, defs...)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := ep.Close(context.Background()); err != nil {
			tb.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := restatex.Register(ctx, cfg.AdminURL, ep.URL()); err != nil {
		tb.Fatal(err)
	}
	return ingress.NewClient(cfg.IngressURL)
}
