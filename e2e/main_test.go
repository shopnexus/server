// Package e2e starts the whole server in-process against a real database and
// calls it over HTTP. Every response is validated against api/openapi.yaml,
// and the run fails when a status an operation declares was never observed.
package e2e

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/api"
	"github.com/shopnexus/server/internal/ai"
	"github.com/shopnexus/server/internal/app"
	"github.com/shopnexus/server/internal/console"
	consoledomain "github.com/shopnexus/server/internal/console/domain"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/provider/firebase/firebasetest"
	"github.com/shopnexus/server/internal/provider/google/googletest"
)

var (
	// server is the base URL of the app every test shares.
	server  string
	discard = slog.New(slog.DiscardHandler)
	db      *pgxpool.Pool                // the e2e database, for setup a test cannot do over HTTP
	keys    = func() config.AuthConfig { // the server's keys, so tests can mint tokens
		k := config.NewAuthKeys()
		k.Issuer, k.Audience = "server-e2e", "shopnexus-app-e2e"
		return k
	}()
	issuer     *firebasetest.Issuer // signs the phone tokens; the server's key URLs point at it
	google     *googletest.Issuer   // signs Google ID tokens; the server's key URL points at it
	model      = newModel()         // the model that reads search queries
	embedder   = newEmbed()         // the alias embed-text, and module ai's photo vectors
	assistStub = newAssist()        // module ai's frames and speech, and the video-read model
	scamStub   = newScam()          // the scam-check model
	chototStub = newChotot()        // Chợ Tốt's gateway and CDN
	agentStub  = newAgent()         // the console agent service
	gateway    = newGateway()       // LiteLLM's admin API
	phones     = newPhones()        // every push the server sends
	mails      = newInbox()         // every mail the server sends
	consoleSvc *consolesvc.Console  // on the server's keys and database, so tests create members directly
	// serverConfig is the shared server's configuration, for a test that
	// starts a second server.
	serverConfig config.Config
	// mcpURL is the shared server's MCP endpoint.
	mcpURL string
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		fmt.Println("e2e: skipped under -short")
		os.Exit(0)
	}
	os.Exit(run(m))
}

func run(m *testing.M) int {
	var closers []func()
	defer func() {
		for _, c := range slices.Backward(closers) {
			c()
		}
	}()
	if err := setup(&closers); err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}

	code := m.Run()
	if code == 0 {
		if missing := uncovered(); len(missing) > 0 {
			fmt.Fprintln(os.Stderr, "e2e: declared responses no test observed:")
			for _, s := range missing {
				fmt.Fprintln(os.Stderr, "  ", s)
			}
			code = 1
		}
	}
	return code
}

// setup starts what the tests share and the server on it, and appends
// what stops each part to closers, in start order.
func setup(closers *[]func()) error {
	adminURL, err := pgtest.AdminURL()
	if err != nil {
		return err
	}
	pool, drop, err := pgtest.NewForMain(adminURL)
	if err != nil {
		return err
	}
	*closers = append(*closers, drop)
	db = pool

	restateCfg, err := restatetest.ConfigForMain()
	if err != nil {
		return err
	}
	*closers = append(*closers, func() { _ = restatetest.Forget(restateCfg.ServicePrefix) })
	objectStoreCfg, err := objectstoretest.Config()
	if err != nil {
		return err
	}
	store, err := objectstore.New(objectStoreCfg)
	if err != nil {
		return err
	}
	if err = store.Prepare(context.Background()); err != nil {
		return err
	}
	signer, stopSigner, err := firebasetest.NewForMain()
	if err != nil {
		return err
	}
	*closers = append(*closers, stopSigner)
	issuer = signer
	googleSigner, stopGoogle, err := googletest.NewForMain()
	if err != nil {
		return err
	}
	*closers = append(*closers, stopGoogle)
	google = googleSigner
	// PNV's keys stay down for the whole run, so a PNV token observes the
	// 503 the spec declares; the firebase tests check PNV tokens.
	issuer.SetPNVDown(true)
	cfg := config.Config{
		DatabaseURL: pool.Config().ConnString(),
		ObjectStore: websiteAt(objectStoreCfg),
		Restate:     restateCfg,
		Auth:        keys,
		Console: config.ConsoleConfig{
			Key: config.NewConsoleKey(), Origin: "http://console.e2e",
			DashboardHosts: map[string]string{"grafana": "grafana.e2e.test", "restate": "restate.e2e.test"},
		},
		LLM: config.LLMConfig{BaseURL: model.url(), APIKey: "e2e", Model: "e2e"},
		Embed: config.EmbedConfig{
			TextBaseURL: embedder.url(), TextModel: "embed-text", TextVersion: "text-test", ImageVersion: "image-test",
			IndexAPIKey: "", SearchAPIKey: "", Timeout: time.Second, Slots: 1,
			TextMinSimilarity: 0.5, PhotoMinSimilarity: 0.5, LookMinSimilarity: 0.05,
		},
		Assist: config.AssistConfig{
			LLMBaseURL: assistStub.url(), LLMAPIKey: "e2e", LLMModel: "e2e", CallTimeout: 5 * time.Second,
			ChototGatewayURL: chototStub.url(), ChototCDNURL: chototStub.url(),
		},
		AI: config.AIConfig{
			EmbedModel: "siglip2-so400m-patch16-384", EmbedMaxInputs: 16, EmbedMaxImageMB: 10, FramesMaxConcurrent: 2,
		},
		Trust: config.TrustConfig{LLMBaseURL: scamStub.url(), LLMAPIKey: "e2e", LLMModel: "e2e", CallTimeout: 5 * time.Second},
		Agent: config.AgentConfig{
			BaseURL: agentStub.url(), ServiceToken: agentServiceToken, MCPAddr: "127.0.0.1:0", MCPToken: agentMCPToken,
			LiteLLMURL: gateway.url(), LiteLLMMasterKey: gatewayMasterKey,
		},
		Firebase: issuer.Config(false),
		Email:    config.EmailConfig{Sender: "log", ResendAPIKey: "", From: "ShopNexus <no-reply@e2e.test>"},
		Google:   google.Config(),
		Carrier:  config.CarrierConfig{Mode: "fake", TokenKey: config.NewConsoleKey(), PollEvery: time.Hour},
		// The website's origin is http, so no listing change is submitted.
		IndexNowKey: indexNowKey,
		// Tests may set X-Forwarded-For.
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	}
	serverConfig = cfg
	consoleKeys, err := consolesvc.NewKeys(cfg.Console, consoledomain.Fast)
	if err != nil {
		return err
	}
	consoleSvc = console.NewService(pool, console.Settings{Keys: consoleKeys, Config: cfg.Console}, nil, time.Now)
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones),
		app.WithEmailSender(mails),
		app.WithPasswordHasher(consoledomain.Fast),
		fakeAI())
	if err != nil {
		return err
	}
	*closers = append(*closers, func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	*closers = append(*closers, srv.Close)
	server = srv.URL
	mcpURL = "http://" + a.AgentMCPAddr().String() + "/mcp"

	return loadSpec()
}

// fakeAI gives module ai the stubs' frames, speech and photo vectors in
// place of the models.
func fakeAI() app.Option {
	return app.WithAIEngines(ai.Engines{Speech: assistStub.models(), Frames: assistStub.models(), Images: embedder.photos()})
}

// uncovered lists "operationId status" for every numeric response code in the
// spec that no call observed.
func uncovered() []string {
	doc, _ := api.Load()
	var out []string
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			for code := range op.Responses.Map() {
				status, err := strconv.Atoi(code)
				if err != nil {
					continue
				}
				if !observed(op.OperationID, status) {
					out = append(out, fmt.Sprintf("%s %d", op.OperationID, status))
				}
			}
		}
	}
	slices.Sort(out)
	return out
}
