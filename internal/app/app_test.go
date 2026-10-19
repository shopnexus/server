package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/jobs"
	"github.com/shopnexus/server2/internal/platform/natstest"
	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/provider/sms"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func testConfig(t *testing.T) config.Config {
	t.Helper()
	nc, _ := natstest.Start(t)
	return config.Config{DatabaseURL: pgtest.New(t).Config().ConnString(), NATSURL: nc.ConnectedUrl()}
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
	a.stopBackground() // the relay holds a pooled connection
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

// syncBuffer is a bytes.Buffer safe for the logger's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSMSJobReachesSender(t *testing.T) {
	var logs syncBuffer
	a := newAppWithLog(t, slog.New(slog.NewTextHandler(&logs, nil)))
	if err := jobs.Enqueue(context.Background(), a.pool, sms.SendCommand{To: "+84901234567", Text: "code 583920"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "583920") {
		if time.Now().After(deadline) {
			t.Fatalf("sms never reached the sender; logs:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReadyzNATSDown(t *testing.T) {
	a := newApp(t)
	a.nc.Close()
	if code, _ := get(a, "/readyz"); code != http.StatusServiceUnavailable {
		t.Fatalf("nats down: %d", code)
	}
}
