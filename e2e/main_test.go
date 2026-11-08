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

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/app"
	"github.com/shopnexus/server2/internal/console"
	consoledomain "github.com/shopnexus/server2/internal/console/domain"
	consolesvc "github.com/shopnexus/server2/internal/console/service"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/provider/firebase/firebasetest"
)

var (
	// server is the base URL of the app every test shares.
	server  string
	discard = slog.New(slog.DiscardHandler)
	db      *pgxpool.Pool                // the e2e database, for setup a test cannot do over HTTP
	keys    = func() config.AuthConfig { // the server's keys, so tests can mint tokens
		k := config.NewAuthKeys()
		k.Issuer, k.Audience = "server2-e2e", "shopnexus-app-e2e"
		return k
	}()
	issuer     *firebasetest.Issuer // signs the phone tokens; the server's key URLs point at it
	model      = newModel()         // the model that reads search queries
	phones     = newPhones()        // every push the server sends
	consoleSvc *consolesvc.Console  // on the server's keys and database, so tests create members directly
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
	// PNV's keys stay down for the whole run, so a PNV token observes the
	// 503 the spec declares; the firebase tests check PNV tokens.
	issuer.SetPNVDown(true)
	cfg := config.Config{
		DatabaseURL: pool.Config().ConnString(),
		ObjectStore: objectStoreCfg,
		Restate:     restateCfg,
		Auth:        keys,
		Console: config.ConsoleConfig{
			Key: config.NewConsoleKey(), Origin: "http://console.e2e",
			DashboardHosts: map[string]string{"grafana": "grafana.e2e.test", "restate": "restate.e2e.test"},
		},
		LLM:      config.LLMConfig{BaseURL: model.url(), APIKey: "e2e", Model: "e2e"},
		Firebase: issuer.Config(false),
		// Tests may set X-Forwarded-For.
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	}
	consoleKeys, err := consolesvc.NewKeys(cfg.Console, consoledomain.Fast)
	if err != nil {
		return err
	}
	consoleSvc = console.NewService(pool, console.Settings{Keys: consoleKeys, Config: cfg.Console}, nil, time.Now)
	a, err := app.New(context.Background(), cfg, discard,
		app.WithPushSender(phones),
		app.WithPasswordHasher(consoledomain.Fast))
	if err != nil {
		return err
	}
	*closers = append(*closers, func() { _ = a.Close(context.Background()) })
	srv := httptest.NewServer(a.Handler)
	*closers = append(*closers, srv.Close)
	server = srv.URL

	return loadSpec()
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
