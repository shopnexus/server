// Package app builds every dependency of the server by hand. cmd/server and
// the e2e tests both start the server through New.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

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
	pool    *pgxpool.Pool
}

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.ready(r.Context()); err != nil {
			log.WarnContext(r.Context(), "not ready", "err", err)
			writeStatus(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeStatus(w, http.StatusOK, "ok")
	})
	a.Handler = httpx.Trace(httpx.AccessLog(log)(mux))
	return a, nil
}

func (a *App) ready(ctx context.Context) error {
	return a.pool.Ping(ctx)
}

func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

// Close releases what New opened.
func (a *App) Close(ctx context.Context) error {
	a.pool.Close()
	return nil
}
