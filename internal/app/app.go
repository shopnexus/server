// Package app builds every dependency of the server by hand. cmd/server and
// the e2e tests both start the server through New.
package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
	"github.com/shopnexus/server2/migrations"
)

// migrationFS is the schema New checks against; tests replace it.
var migrationFS fs.FS = migrations.FS

// App is the assembled server.
type App struct {
	Handler  http.Handler
	router   *httpx.Router
	pool     *pgxpool.Pool
	cfg      config.Config
	opts     options
	ingress  *ingress.Client
	endpoint *restatex.Endpoint
}

// Option changes how New builds the app.
type Option func(*options)

type options struct {
	sms           sms.Sender
	email         email.Sender
	restatePrefix string
}

// WithSMSSender replaces the log sender; E2E tests read codes from theirs.
func WithSMSSender(s sms.Sender) Option { return func(o *options) { o.sms = s } }

// WithEmailSender replaces the log sender.
func WithEmailSender(s email.Sender) Option { return func(o *options) { o.email = s } }

// WithRestatePrefix prefixes every Restate service name, so tests sharing one
// Restate server do not route to each other's endpoints.
func WithRestatePrefix(p string) Option { return func(o *options) { o.restatePrefix = p } }

// Routes lists every registered route, for the route-versus-spec test.
func (a *App) Routes() []httpx.Route { return a.router.Routes() }

// New builds the server for cfg and starts its background work. It refuses to
// start while migrations are pending, so the server never runs against an
// older schema.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, opts ...Option) (*App, error) {
	if len(cfg.Auth.JWTKey) != ed25519.PrivateKeySize || len(cfg.Auth.OTPKey) < 32 {
		return nil, errors.New("auth keys missing from config; run make keys")
	}
	o := options{sms: sms.LogSender{Log: log}, email: email.LogSender{Log: log}}
	for _, opt := range opts {
		opt(&o)
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	a := &App{pool: pool, cfg: cfg, opts: o, ingress: ingress.NewClient(cfg.Restate.IngressURL)}

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
		a.Close(context.Background())
		return nil, fmt.Errorf("load api spec: %w", err)
	}
	validate, err := httpx.ValidateRequests(doc, log)
	if err != nil {
		a.Close(context.Background())
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

	if err := a.startRestate(ctx, cfg, log, nil); err != nil {
		a.Close(context.Background())
		return nil, err
	}

	// Trace is outermost so a panic's 500 still carries a trace id.
	a.Handler = httpx.Trace(httpx.Recover(log)(httpx.AccessLog(log)(validate(a.router))))
	return a, nil
}

type status struct {
	Status string `json:"status"`
}

// startRestate serves defs and registers them, retrying for up to 30 s while
// the Restate server starts. With no services there is nothing to serve.
func (a *App) startRestate(ctx context.Context, cfg config.Config, log *slog.Logger, defs []restate.ServiceDefinition) error {
	if len(defs) == 0 {
		return nil
	}
	ep, err := restatex.Serve(cfg.Restate.ListenAddr, defs...)
	if err != nil {
		return fmt.Errorf("restate endpoint: %w", err)
	}
	a.endpoint = ep
	url := cfg.Restate.EndpointURL
	if url == "" {
		url = ep.URL()
	}
	for attempt := range 15 {
		if err = restatex.Register(ctx, cfg.Restate.AdminURL, url); err == nil {
			return nil
		}
		log.WarnContext(ctx, "restate not reachable yet", "attempt", attempt+1, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return err
}

func (a *App) ready(ctx context.Context) error {
	if err := a.pool.Ping(ctx); err != nil {
		return err
	}
	return restatex.Healthy(ctx, a.cfg.Restate.AdminURL)
}

// Close stops the Restate endpoint and closes the pool.
func (a *App) Close(ctx context.Context) error {
	if a.endpoint != nil {
		a.endpoint.Close(ctx)
	}
	a.pool.Close()
	return nil
}
