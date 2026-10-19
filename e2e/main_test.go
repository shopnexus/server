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
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/app"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/natstest"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

var (
	// server is the base URL of the app every test shares.
	server  string
	discard = slog.New(slog.DiscardHandler)
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
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "e2e: TEST_DATABASE_URL is not set: run `make test-up` and use `make test-e2e`")
		return 1
	}
	pool, drop, err := pgtest.NewForMain(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer drop()

	natsURL, stopNATS, err := natstest.StartForMain()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer stopNATS()

	cfg := config.Config{DatabaseURL: pool.Config().ConnString(), NATSURL: natsURL}
	a, err := app.New(context.Background(), cfg, discard)
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
