// Package app builds every dependency of the server by hand. cmd/server and
// the e2e tests both start the server through New.
package app

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
	"github.com/shopnexus/server2/migrations"
)

// App is the assembled server.
type App struct {
	Handler  http.Handler
	router   *httpx.Router
	pool     *pgxpool.Pool
	endpoint *restatex.Endpoint
	adminURL string
}

// Option changes how New builds the app.
type Option func(*options)

type options struct {
	sms   sms.Sender
	email email.Sender
}

// WithSMSSender replaces the log sender; E2E tests read codes from theirs.
func WithSMSSender(s sms.Sender) Option { return func(o *options) { o.sms = s } }

// WithEmailSender replaces the log sender.
func WithEmailSender(s email.Sender) Option { return func(o *options) { o.email = s } }

// Routes lists every registered route, for the route-versus-spec test.
func (a *App) Routes() []httpx.Route { return a.router.Routes() }

// New builds the server for cfg and starts its background work. It refuses to
// start while migrations are pending, so the server never runs against an
// older schema.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, opts ...Option) (_ *App, err error) {
	// config.Load checks the keys too; this catches a Config built by hand.
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
	a := &App{router: httpx.NewRouter(), pool: pool, adminURL: cfg.Restate.AdminURL}
	defer func() {
		if err != nil {
			a.Close(context.Background())
		}
	}()

	if err := checkMigrations(ctx, pool); err != nil {
		return nil, err
	}
	doc, err := api.Load()
	if err != nil {
		return nil, fmt.Errorf("load api spec: %w", err)
	}
	validate, err := httpx.ValidateRequests(doc, log)
	if err != nil {
		return nil, err
	}

	a.router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.Respond(w, http.StatusOK, status{"ok"})
	})
	a.router.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.ready(r.Context()); err != nil {
			log.WarnContext(r.Context(), "not ready", "err", err)
			httpx.Respond(w, http.StatusServiceUnavailable, status{"unavailable"})
			return
		}
		httpx.Respond(w, http.StatusOK, status{"ok"})
	})
	defs := routeModules(a.router, pool, cfg, o, log)
	if a.endpoint, err = startRestate(ctx, cfg.Restate, log, defs); err != nil {
		return nil, err
	}

	// Trace is outermost so a panic's 500 still carries a trace id.
	a.Handler = httpx.Trace(httpx.Recover(log)(httpx.AccessLog(log)(validate(a.router))))
	return a, nil
}

type status struct {
	Status string `json:"status"`
}

func checkMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	pending, err := postgres.Pending(ctx, pool, migrations.FS)
	if err != nil || len(pending) == 0 {
		return err
	}
	names := make([]string, len(pending))
	for i, m := range pending {
		names[i] = m.File()
	}
	return fmt.Errorf("migrations pending: %s; run make migrate", strings.Join(names, ", "))
}

// routeModules builds the modules' adapters, registers their routes and
// returns their Restate services.
func routeModules(router *httpx.Router, pool *pgxpool.Pool, cfg config.Config, o options, log *slog.Logger) []restate.ServiceDefinition {
	accounts := account.NewPGStore(pool)
	ports := auth.Ports{
		Accounts:    account.Client{Store: accounts, Now: time.Now},
		Challenges:  otp.NewChallengePGStore(pool),
		Quotas:      otp.NewQuotaPGStore(pool),
		Credentials: credentials.NewPGStore(pool),
		Sessions:    session.NewPGStore(pool),
		SMS:         o.sms,
		Email:       o.email,
	}
	keys := auth.Keys{
		Codes:    otp.NewCodes(cfg.Auth.OTPKey),
		Signer:   session.NewSigner(cfg.Auth),
		Verifier: session.NewVerifier(cfg.Auth),
	}
	prefix := cfg.Restate.ServicePrefix
	rs := auth.Restate{Client: ingress.NewClient(cfg.Restate.IngressURL), Prefix: prefix}
	auth.Routes(router, ports, keys, rs, cfg.TrustedProxies, time.Now, log)
	account.Routes(router, accounts, time.Now, log, auth.RequireAuth(keys.Verifier, time.Now, log))
	return auth.Services(prefix, ports, keys)
}

// startRestate serves defs and registers them with Restate.
func startRestate(ctx context.Context, cfg config.RestateConfig, log *slog.Logger, defs []restate.ServiceDefinition) (*restatex.Endpoint, error) {
	ep, err := restatex.Serve(cfg.ListenAddr, defs...)
	if err != nil {
		return nil, fmt.Errorf("restate endpoint: %w", err)
	}
	url := cfg.EndpointURL
	if url == "" {
		url = ep.URL()
	}
	if err := register(ctx, cfg.AdminURL, url, log); err != nil {
		ep.Close(context.Background())
		return nil, err
	}
	return ep, nil
}

// register retries for up to 30 s while the Restate server starts.
func register(ctx context.Context, adminURL, url string, log *slog.Logger) error {
	var err error
	for attempt := range 15 {
		if err = restatex.Register(ctx, adminURL, url); err == nil {
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
	return restatex.Healthy(ctx, a.adminURL)
}

// Close stops the Restate endpoint and closes the pool.
func (a *App) Close(ctx context.Context) error {
	if a.endpoint != nil {
		a.endpoint.Close(ctx)
	}
	a.pool.Close()
	return nil
}
