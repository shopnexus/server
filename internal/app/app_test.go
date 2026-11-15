package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/provider/firebase/firebasetest"
	"github.com/shopnexus/server2/internal/provider/google/googletest"
	"github.com/shopnexus/server2/internal/provider/push"
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
		Console:     config.ConsoleConfig{Key: config.NewConsoleKey(), Origin: "http://console.test"},
		ObjectStore: objectStore(t),
		// No model answers here; a search falls back to the raw words.
		LLM: config.LLMConfig{BaseURL: "http://127.0.0.1:1", APIKey: "none", Model: "none"},
		Embed: config.EmbedConfig{
			TextBaseURL: "http://127.0.0.1:1", ImageBaseURL: "http://127.0.0.1:1",
			TextModel: "embed-text", ImageModel: "embed-image", TextVersion: "text-test", ImageVersion: "image-test",
			IndexAPIKey: "", SearchAPIKey: "", Timeout: time.Second, Slots: 1,
			TextMinSimilarity: 0.5, PhotoMinSimilarity: 0.5,
		},
		Assist: config.AssistConfig{
			FramesBaseURL: "http://127.0.0.1:1/v1", STTBaseURL: "http://127.0.0.1:1/v1", STTAPIKey: "", STTModel: "none",
			LLMBaseURL: "http://127.0.0.1:1", LLMAPIKey: "none", LLMModel: "none", CallTimeout: time.Second,
		},
		Trust:    config.TrustConfig{LLMBaseURL: "http://127.0.0.1:1", LLMAPIKey: "none", LLMModel: "none", CallTimeout: time.Second},
		Firebase: firebasetest.New(t).Config(false),
		Email:    config.EmailConfig{Sender: "log", ResendAPIKey: "", From: "ShopNexus <no-reply@shopnexus.test>"},
		Google:   googletest.New(t).Config(),
		Carrier:  config.CarrierConfig{Mode: "fake", TokenKey: config.NewConsoleKey(), PollEvery: time.Hour},
	}
}

// objectStore is the test stack's object store, its bucket prepared.
func objectStore(t *testing.T) config.ObjectStoreConfig {
	t.Helper()
	objectstoretest.New(t)
	cfg, err := objectstoretest.Config()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
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

// Without FCM credentials pushes go to the log, which production must not
// do in silence.
func TestNewRefusesLogPushInProd(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Env = "prod"
	_, err := New(context.Background(), cfg, discard())
	if err == nil || !strings.Contains(err.Error(), "FCM_CREDENTIALS") {
		t.Fatalf("err %v", err)
	}
}

type quietPush struct{}

func (quietPush) Send(context.Context, string, push.Message) error { return nil }

// Test numbers of PNV sign anyone in as the zero number, so production
// refuses them.
func TestNewRefusesPNVTestNumbersInProd(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Env, cfg.Firebase.TestNumbers = "prod", true
	_, err := New(context.Background(), cfg, discard(), WithPushSender(quietPush{}))
	if err == nil || !strings.Contains(err.Error(), "PNV_TEST_NUMBERS") {
		t.Fatalf("err %v", err)
	}
}

// The log sender writes every code to the log, so production must name a
// real one.
func TestNewRefusesLogEmailInProd(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Env = "prod"
	_, err := New(context.Background(), cfg, discard(), WithPushSender(quietPush{}))
	if err == nil || !strings.Contains(err.Error(), "EMAIL_SENDER") {
		t.Fatalf("err %v", err)
	}
}

// A Config built by hand without OTP_KEY would hash every code with no key.
func TestNewRefusesAMissingOTPKey(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Auth.OTPKey = nil
	_, err := New(context.Background(), cfg, discard())
	if err == nil || !strings.Contains(err.Error(), "OTP_KEY") {
		t.Fatalf("err %v", err)
	}
}

// New runs the event bus and the stream hub, and Close waits for both to
// return before closing the pool.
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
}

func TestNewRefusesAnUnknownCarrier(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Carrier.Mode = "pigeon"
	if _, err := New(context.Background(), cfg, discard()); err == nil || !strings.Contains(err.Error(), "CARRIER") {
		t.Fatalf("err %v", err)
	}
}

// With the fake carrier anyone may move shipments through the dev
// endpoint, so production refuses it.
func TestNewRefusesTheFakeCarrierInProd(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Env, cfg.Email.Sender, cfg.Email.ResendAPIKey = "prod", "resend", "re_test"
	_, err := New(context.Background(), cfg, discard(), WithPushSender(quietPush{}))
	if err == nil || !strings.Contains(err.Error(), "CARRIER") {
		t.Fatalf("err %v", err)
	}
}

// Without a carrier the dev endpoint that moves shipments is not served;
// the shipment routes are, and so is the dev endpoint that logs updates.
func TestNoCarrierServesNoDevEndpoint(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Carrier.Mode = "none"
	a, err := New(context.Background(), cfg, discard())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	routes := a.Routes()
	if slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/dev/shipments/{code}/status"}) ||
		!slices.Contains(routes, httpx.Route{Method: "GET", Path: "/v1/shipments/lookup"}) ||
		!slices.Contains(routes, httpx.Route{Method: "PUT", Path: "/v1/carrier-accounts/{carrier}"}) ||
		!slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/carriers/{carrier}/webhook/{secret}"}) ||
		!slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/dev/updates"}) {
		t.Fatalf("routes %v", routes)
	}
}

// Production does not serve the dev endpoint that logs updates.
func TestProdServesNoDevUpdates(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Env, cfg.Email.Sender, cfg.Email.ResendAPIKey, cfg.Carrier.Mode = "prod", "resend", "re_test", "none"
	a, err := New(context.Background(), cfg, discard(), WithPushSender(quietPush{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	routes := a.Routes()
	if slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/dev/updates"}) ||
		!slices.Contains(routes, httpx.Route{Method: "GET", Path: "/v1/updates"}) {
		t.Fatalf("routes %v", routes)
	}
}
