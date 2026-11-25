package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/ai"
	"github.com/shopnexus/server/internal/ai/aitest"
	aidomain "github.com/shopnexus/server/internal/ai/domain"
	aisvc "github.com/shopnexus/server/internal/ai/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/provider/firebase/firebasetest"
	"github.com/shopnexus/server/internal/provider/google/googletest"
	"github.com/shopnexus/server/internal/provider/push"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func testConfig(t *testing.T) config.Config {
	t.Helper()
	keys := config.NewAuthKeys()
	keys.Issuer, keys.Audience = "server-test", "shopnexus-app-test"
	return config.Config{
		DatabaseURL: pgtest.New(t).Config().ConnString(), DatabaseMaxConns: 20,
		Restate:     restatetest.Config(t),
		Auth:        keys,
		Console:     config.ConsoleConfig{Key: config.NewConsoleKey(), Origin: "http://console.test"},
		ObjectStore: objectStore(t),
		// No model answers here; a search falls back to the raw words.
		LLM: config.LLMConfig{BaseURL: "http://127.0.0.1:1", APIKey: "none", Model: "none"},
		Embed: config.EmbedConfig{
			TextBaseURL: "http://127.0.0.1:1", TextModel: "embed-text", TextVersion: "text-test", ImageVersion: "image-test",
			IndexAPIKey: "", SearchAPIKey: "", Timeout: time.Second, Slots: 1,
			TextMinSimilarity: 0.5, PhotoMinSimilarity: 0.5,
		},
		AI: config.AIConfig{
			EmbedModel: "siglip2", EmbedMaxInputs: 4, EmbedMaxImageMB: 1, FramesMaxConcurrent: 1,
		},
		Assist: config.AssistConfig{
			LLMBaseURL: "http://127.0.0.1:1", LLMAPIKey: "none", LLMModel: "none", CallTimeout: time.Second,
		},
		Trust: config.TrustConfig{LLMBaseURL: "http://127.0.0.1:1", LLMAPIKey: "none", LLMModel: "none", CallTimeout: time.Second},
		Agent: config.AgentConfig{
			BaseURL: "http://127.0.0.1:1", ServiceToken: "app-test-agent-service-token-0123456789",
			MCPAddr: "127.0.0.1:0", MCPToken: "app-test-agent-mcp-token-0123456789abcdef",
			LiteLLMURL: "http://127.0.0.1:1", LiteLLMMasterKey: "none",
		},
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
	a, err := New(context.Background(), testConfig(t), log, withFakeAI())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	return a
}

// withFakeAI gives module ai fake engines in place of the models.
func withFakeAI() Option {
	f := aitest.Fake{Dims: catalogdomain.PhotoDims}
	return WithAIEngines(ai.Engines{Speech: f, Frames: f, Images: f})
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
	a, err := New(context.Background(), cfg, discard(), withFakeAI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	routes := a.Routes()
	if slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/dev/shipments/{code}/status"}) ||
		!slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/orders/{order_id}/shipment"}) ||
		!slices.Contains(routes, httpx.Route{Method: "PUT", Path: "/v1/carrier-accounts/{carrier}"}) ||
		!slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/carriers/{carrier}/webhook/{secret}"}) ||
		!slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/dev/updates"}) {
		t.Fatalf("routes %v", routes)
	}
}

// Production mounts neither dev route, and a request for one answers as an
// unknown path does, whatever its body: the request validator matches only
// mounted routes, so it reveals nothing about the spec's dev operations.
func TestProdServesNoDevRoute(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Env, cfg.Email.Sender, cfg.Email.ResendAPIKey, cfg.Carrier.Mode = "prod", "resend", "re_test", "none"
	a, err := New(context.Background(), cfg, discard(), WithPushSender(quietPush{}), withFakeAI())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	routes := a.Routes()
	if slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/dev/updates"}) ||
		slices.Contains(routes, httpx.Route{Method: "POST", Path: "/v1/dev/shipments/{code}/status"}) ||
		!slices.Contains(routes, httpx.Route{Method: "GET", Path: "/v1/updates"}) {
		t.Fatalf("routes %v", routes)
	}
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.Handler.ServeHTTP(rec, req)
		return rec
	}
	unknown := post("/v1/dev/nothing", `{}`)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("an unknown path: %d %s", unknown.Code, unknown.Body)
	}
	for _, path := range []string{"/v1/dev/updates", "/v1/dev/shipments/S1/status"} {
		for _, body := range []string{
			``, `{`, `{}`, `{"kind":"typing"}`, `{"kind":"deal","deal_id":"01928c3e-7b5a-7c1e-8a2b-3c4d5e6f7a8b"}`,
			`{"status":"delivered"}`, `{"status":"returned"}`,
		} {
			rec := post(path, body)
			if rec.Code != unknown.Code || rec.Body.String() != unknown.Body.String() ||
				rec.Header().Get("Content-Type") != unknown.Header().Get("Content-Type") {
				t.Errorf("%s %q: %d %s, want %d %s", path, body, rec.Code, rec.Body, unknown.Code, unknown.Body)
			}
		}
	}
}

// SERVER_ROLE=ai serves module ai alone: the probes on HTTP, and the
// Restate service ai, which the client reaches through the ingress.
func TestRoleAI(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Role = config.RoleAI
	a, err := New(context.Background(), cfg, discard(), withFakeAI())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	if a.pool != nil {
		t.Fatal("role ai opened the database")
	}
	routes := a.Routes()
	if len(routes) != 2 {
		t.Fatalf("routes %v, want the probes alone", routes)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		if code, body := get(a, path); code != http.StatusOK {
			t.Fatalf("%s: %d %q", path, code, body)
		}
	}
	client := aisvc.NewClient(ingress.NewClient(cfg.Restate.IngressURL), cfg.Restate.ServicePrefix)
	vs, err := client.EmbedImages(context.Background(), []string{"data:image/jpeg;base64,AAAA"})
	if err != nil || len(vs) != 1 || len(vs[0]) != catalogdomain.PhotoDims {
		t.Fatalf("EmbedImages: %d vectors, %v", len(vs), err)
	}
	if _, err = client.Transcribe(context.Background(), "media/none"); !errors.Is(err, aidomain.ErrNotFound) {
		t.Fatalf("Transcribe of a missing key: %v", err)
	}
}

// SERVER_ROLE=api loads no model and leaves service ai to another process.
func TestRoleAPI(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t)
	cfg.Role = config.RoleAPI
	// No engines: a model load would fail on the empty AI config.
	a, err := New(context.Background(), cfg, discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { a.Close(context.Background()) })
	client := aisvc.NewClient(ingress.NewClient(cfg.Restate.IngressURL), cfg.Restate.ServicePrefix)
	_, err = client.EmbedImages(context.Background(), []string{"data:image/jpeg;base64,AAAA"})
	if err == nil || errors.Is(err, aidomain.ErrRefused) {
		t.Fatalf("service ai answered in role api: %v", err)
	}
}
