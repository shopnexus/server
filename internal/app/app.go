// Package app builds every dependency of the server by hand. cmd/server and
// the e2e tests both start the server through New.
package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// App is the assembled server.
type App struct {
	Handler http.Handler
}

// New builds the server for cfg.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	return &App{Handler: httpx.Trace(httpx.AccessLog(log)(mux))}, nil
}

// Close releases what New opened.
func (a *App) Close(ctx context.Context) error {
	return nil
}
