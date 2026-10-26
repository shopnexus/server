package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func testConfig(t *testing.T) config.Config {
	t.Helper()
	keys := config.NewAuthKeys()
	keys.Issuer, keys.Audience = "server2-test", "shopnexus-app-test"
	return config.Config{
		DatabaseURL: pgtest.New(t).Config().ConnString(),
		Restate:     restatetest.Config(t),
		Auth:        keys,
		Staff:       config.StaffConfig{Key: config.NewStaffKey(), ConsoleOrigin: "http://console.test"},
	}
}

func newApp(t *testing.T) *App {
	t.Helper()
	return newAppWithLog(t, discard())
}

func newAppWithLog(t *testing.T, log *slog.Logger) *App {
	t.Helper()
	a, err := New(context.Background(), testConfig(t), log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	return a
}

func get(a *App, path string) (status int, body string) {
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody))
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b)
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	code, body := get(newApp(t), "/healthz")
	if code != http.StatusOK || body != `{"status":"ok"}`+"\n" {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	cfg := testConfig(t)
	cfg.DatabaseURL = pgtest.Empty(t).Config().ConnString()
	_, err := New(context.Background(), cfg, discard())
	if err == nil || !strings.Contains(err.Error(), "cmd/migrate") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadyzRestateDown(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	// The app registered its services at start; readiness now asks an admin
	// API that no longer answers.
	a.adminURL = "http://127.0.0.1:1"
	if code, body := get(a, "/readyz"); code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestNewRefusesMissingKeys(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Auth = config.AuthConfig{}
	if _, err := New(context.Background(), cfg, discard()); err == nil || !strings.Contains(err.Error(), "make run") {
		t.Fatalf("err %v", err)
	}
}

// The log senders write OTP codes to the log, so production must name real
// senders.
func TestNewRefusesLogSendersInProd(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Env = "prod"
	_, err := New(context.Background(), cfg, discard())
	if err == nil || !strings.Contains(err.Error(), "log sender") {
		t.Fatalf("err %v", err)
	}
}

// New runs the event bus and Close stops it before closing the pool.
func TestCloseStopsTheEventBus(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	closed := make(chan error, 1)
	go func() { closed <- a.Close(context.Background()) }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	select {
	case <-a.eventsDone:
	default:
		t.Fatal("the event bus still runs")
	}
}
