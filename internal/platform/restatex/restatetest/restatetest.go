// Package restatetest connects tests to the test stack's Restate server.
package restatetest

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/platform/testenv"
)

// urls returns TEST_RESTATE_INGRESS_URL and TEST_RESTATE_ADMIN_URL, from the
// environment or .env.test.
func urls() (ingressURL, adminURL string, err error) {
	if ingressURL, err = testenv.Get("TEST_RESTATE_INGRESS_URL"); err != nil {
		return "", "", fmt.Errorf("test restate: %w", err)
	}
	if adminURL, err = testenv.Get("TEST_RESTATE_ADMIN_URL"); err != nil {
		return "", "", fmt.Errorf("test restate: %w", err)
	}
	return ingressURL, adminURL, nil
}

// identityFile is the key the test stack's Restate signs every request with:
// TEST_RESTATE_IDENTITY_FILE, or the one make test writes and mounts.
func identityFile() string {
	if path := os.Getenv("TEST_RESTATE_IDENTITY_FILE"); path != "" {
		return path
	}
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
	ingressURL, adminURL, err := urls()
	if err != nil {
		return config.RestateConfig{}, err
	}
	return config.RestateConfig{
		IngressURL: ingressURL, AdminURL: adminURL, ListenAddr: "127.0.0.1:0", EndpointURL: "",
		ServicePrefix: Prefix(), IdentityKey: key, Register: true,
	}, nil
}

// Config is ConfigForMain for one test, which it skips under -short. The
// deployments registered under its prefix go when the test ends.
func Config(tb testing.TB) config.RestateConfig {
	tb.Helper()
	if testing.Short() {
		tb.Skip("needs Restate")
	}
	cfg, err := ConfigForMain()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := Forget(cfg.ServicePrefix); err != nil {
			tb.Error(err)
		}
	})
	return cfg
}

// Forget deletes the test stack's deployments whose services carry prefix.
// Every run registers services of its own, and Restate keeps each one in
// memory until its deployment is deleted.
func Forget(prefix string) error {
	return deleteDeployments(func(d deployment) bool {
		return len(d.Services) > 0 && strings.HasPrefix(d.Services[0].Name, prefix)
	})
}

type deployment struct {
	ID       string `json:"id"`
	URI      string `json:"uri"`
	Services []struct {
		Name string `json:"name"`
	} `json:"services"`
}

// deleteDeployments deletes the deployments that match, forcing out any invocation
// a failed test left behind.
func deleteDeployments(match func(deployment) bool) error {
	_, adminURL, err := urls()
	if err != nil {
		return err
	}
	admin := strings.TrimRight(adminURL, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, admin+"/deployments", http.NoBody)
	if err != nil {
		return fmt.Errorf("list restate deployments: %w", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("list restate deployments: %w", err)
	}
	var list struct {
		Deployments []deployment `json:"deployments"`
	}
	err = json.NewDecoder(res.Body).Decode(&list)
	_ = res.Body.Close()
	if err != nil {
		return fmt.Errorf("list restate deployments: %w", err)
	}
	for _, d := range list.Deployments {
		if !match(d) {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, admin+"/deployments/"+d.ID+"?force=true", http.NoBody)
		if err != nil {
			return fmt.Errorf("delete restate deployment %s: %w", d.ID, err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("delete restate deployment %s: %w", d.ID, err)
		}
		_ = res.Body.Close()
		if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
			return fmt.Errorf("delete restate deployment %s: %s", d.ID, res.Status)
		}
	}
	return nil
}

// Ingress is a client of the test stack's Restate ingress. It skips the
// test under -short, as Config does.
func Ingress(tb testing.TB) *ingress.Client {
	tb.Helper()
	if testing.Short() {
		tb.Skip("needs Restate")
	}
	ingressURL, _, err := urls()
	if err != nil {
		tb.Fatal(err)
	}
	return ingress.NewClient(ingressURL)
}

// Prefix returns a fresh service-name prefix. Test packages run in parallel
// against one Restate server, and a service name routes to one endpoint, so
// every registration needs names of its own.
func Prefix() string {
	return "t" + rand.Text()[:8] + "_"
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
	// Runs before the endpoint closes: cleanups go last in, first out.
	tb.Cleanup(func() {
		if err := deleteDeployments(func(d deployment) bool { return d.URI == ep.URL() || d.URI == ep.URL()+"/" }); err != nil {
			tb.Error(err)
		}
	})
	return ingress.NewClient(cfg.IngressURL)
}
