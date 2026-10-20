package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatetest"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func testConfig(t *testing.T) config.Config {
	t.Helper()
	ingressURL, adminURL := restatetest.URLs(t)
	return config.Config{
		DatabaseURL: pgtest.New(t).Config().ConnString(),
		Restate:     config.RestateConfig{IngressURL: ingressURL, AdminURL: adminURL, ListenAddr: "127.0.0.1:0"},
		Auth:        config.NewAuthKeys(),
	}
}

func newApp(t *testing.T) *App {
	t.Helper()
	return newAppWithLog(t, discard())
}

func newAppWithLog(t *testing.T, log *slog.Logger) *App {
	t.Helper()
	a, err := New(context.Background(), testConfig(t), log, WithRestatePrefix(restatetest.Prefix()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	return a
}

func get(a *App, path string) (int, string) {
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

func TestHealthz(t *testing.T) {
	code, body := get(newApp(t), "/healthz")
	if code != http.StatusOK || body != `{"status":"ok"}`+"\n" {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestReadyz(t *testing.T) {
	a := newApp(t)
	if code, _ := get(a, "/readyz"); code != http.StatusOK {
		t.Fatalf("up: %d", code)
	}
	a.pool.Close()
	if code, _ := get(a, "/readyz"); code != http.StatusServiceUnavailable {
		t.Fatalf("down: %d", code)
	}
}

func TestNewRefusesPendingMigrations(t *testing.T) {
	// migrations/ may hold no files yet; give New one it has not applied.
	prev := migrationFS
	migrationFS = fstest.MapFS{"0001_a.sql": {Data: []byte("CREATE TABLE a (id int);")}}
	t.Cleanup(func() { migrationFS = prev })

	cfg := testConfig(t)
	cfg.DatabaseURL = pgtest.Empty(t).Config().ConnString()
	_, err := New(context.Background(), cfg, discard())
	if err == nil || !strings.Contains(err.Error(), "make migrate") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadyzRestateDown(t *testing.T) {
	cfg := testConfig(t)
	cfg.Restate.AdminURL = "http://127.0.0.1:1"
	a, err := New(context.Background(), cfg, discard())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	if code, body := get(a, "/readyz"); code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestNewRefusesMissingKeys(t *testing.T) {
	cfg := testConfig(t)
	cfg.Auth = config.AuthConfig{}
	if _, err := New(context.Background(), cfg, discard()); err == nil || !strings.Contains(err.Error(), "make keys") {
		t.Fatalf("err %v", err)
	}
}
