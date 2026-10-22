package e2e

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server2/internal/app"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatetest"
)

func TestHealthz(t *testing.T) {
	t.Parallel()
	if res := call(t, server, "GET", "/healthz", nil); res.Status != 200 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()
	if res := call(t, server, "GET", "/readyz", nil); res.Status != 200 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestReadyzDown(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		DatabaseURL: pgtest.New(t).Config().ConnString(),
		Restate:     restatetest.Config(t),
		Auth:        keys,
	}
	a, err := app.New(context.Background(), cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	a.Close(context.Background()) // the pool is gone; readiness must say so

	if res := call(t, srv.URL, "GET", "/readyz", nil); res.Status != 503 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}
