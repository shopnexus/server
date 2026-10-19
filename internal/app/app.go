// Package app builds every dependency of the server by hand. cmd/server and
// the e2e tests both start the server through New.
package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/migrations"
)

// migrationFS is the schema New checks against; tests replace it.
var migrationFS fs.FS = migrations.FS

// App is the assembled server.
type App struct {
	Handler http.Handler
	router  *httpx.Router
	pool    *pgxpool.Pool
}

// Routes lists every registered route, for the route-versus-spec test.
func (a *App) Routes() []httpx.Route { return a.router.Routes() }

// New builds the server for cfg. It refuses to start while migrations are
// pending, so the server never runs against an older schema.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	a := &App{pool: pool}

	pending, err := postgres.Pending(ctx, pool, migrationFS)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if len(pending) > 0 {
		pool.Close()
		names := make([]string, len(pending))
		for i, m := range pending {
			names[i] = m.File()
		}
		return nil, fmt.Errorf("migrations pending: %s; run make migrate", strings.Join(names, ", "))
	}

	doc, err := api.Load()
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("load api spec: %w", err)
	}
	validate, err := httpx.ValidateRequests(doc, log)
	if err != nil {
		pool.Close()
		return nil, err
	}

	a.router = httpx.NewRouter()
	a.router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.Respond(w, r, http.StatusOK, status{"ok"})
	})
	a.router.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.ready(r.Context()); err != nil {
			log.WarnContext(r.Context(), "not ready", "err", err)
			httpx.Respond(w, r, http.StatusServiceUnavailable, status{"unavailable"})
			return
		}
		httpx.Respond(w, r, http.StatusOK, status{"ok"})
	})

	// Trace is outermost so a panic's 500 still carries a trace id.
	a.Handler = httpx.Trace(httpx.Recover(log)(httpx.AccessLog(log)(validate(a.router))))
	return a, nil
}

type status struct {
	Status string `json:"status"`
}

func (a *App) ready(ctx context.Context) error {
	return a.pool.Ping(ctx)
}

// Close releases what New opened.
func (a *App) Close(ctx context.Context) error {
	a.pool.Close()
	return nil
}
