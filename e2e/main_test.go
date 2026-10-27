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
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/staff"
	staffdomain "github.com/shopnexus/server2/internal/staff/domain"
	staffsvc "github.com/shopnexus/server2/internal/staff/service"
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
	box      = newInbox()    // every SMS and email the server sends
	model    = newModel()    // the model that reads search queries
	staffSvc *staffsvc.Staff // on the server's keys and database, so tests create members directly
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
	adminURL, err := pgtest.AdminURL()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	pool, drop, err := pgtest.NewForMain(adminURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer drop()
	db = pool

	restateCfg, err := restatetest.ConfigForMain()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	objectStoreCfg, err := objectstoretest.Config()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	store, err := objectstore.New(objectStoreCfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	if err = store.Prepare(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	cfg := config.Config{
		DatabaseURL: pool.Config().ConnString(),
		ObjectStore: objectStoreCfg,
		Restate:     restateCfg,
		Auth:        keys,
		Staff:       config.StaffConfig{Key: config.NewStaffKey(), ConsoleOrigin: "http://console.e2e"},
		LLM:         config.LLMConfig{BaseURL: model.url(), APIKey: "e2e", Model: "e2e"},
		// Tests set X-Forwarded-For, so each gets its own per-address quota.
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
	}
	staffKeys, err := staffsvc.NewKeys(cfg.Staff, staffdomain.Fast)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	staffSvc = staff.NewService(pool, staffKeys, nil, nil, time.Now)
	a, err := app.New(context.Background(), cfg, discard,
		app.WithSMSSender(box.sms()), app.WithEmailSender(box.email()), app.WithPasswordHasher(staffdomain.Fast))
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer a.Close(context.Background())
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()
	server = srv.URL

	if err := loadSpec(); err != nil {
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
