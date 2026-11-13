package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/app"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

func TestHealthz(t *testing.T) {
	t.Parallel()
	expectStatus(t, call(t, "GET", "/healthz", nil), 200)
}

func TestReadyz(t *testing.T) {
	t.Parallel()
	expectStatus(t, call(t, "GET", "/readyz", nil), 200)
}

func TestReadyzDown(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		DatabaseURL: pgtest.New(t).Config().ConnString(),
		Restate:     restatetest.Config(t),
		Auth:        keys,
		Console:     config.ConsoleConfig{Key: config.NewConsoleKey(), Origin: "http://console.e2e"},
		Carrier:     config.CarrierConfig{Mode: "fake", TokenKey: config.NewConsoleKey(), PollEvery: time.Hour},
	}
	objectStore, err := objectstoretest.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.ObjectStore = objectStore
	a, err := app.New(context.Background(), cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	a.Close(context.Background()) // the pool is gone; readiness must say so

	expectStatus(t, callAt(t, srv.URL, "GET", "/readyz", nil), 503)
}
