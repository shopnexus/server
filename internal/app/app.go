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
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
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
	sms    sms.Sender
	email  email.Sender
	hasher member.Hasher
}

// WithSMSSender replaces the log sender; E2E tests read codes from theirs.
func WithSMSSender(s sms.Sender) Option { return func(o *options) { o.sms = s } }

// WithEmailSender replaces the log sender.
func WithEmailSender(s email.Sender) Option { return func(o *options) { o.email = s } }

// WithPasswordHasher sets the argon2id parameters of new staff passwords;
// tests pass member.Fast.
func WithPasswordHasher(h member.Hasher) Option { return func(o *options) { o.hasher = h } }

// Routes lists every registered route, for the route-versus-spec test.
func (a *App) Routes() []httpx.Route { return a.router.Routes() }

// New builds the server for cfg and starts its background work. It refuses to
// start while migrations are pending, so the server never runs against an
// older schema.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, opts ...Option) (_ *App, err error) {
	// config.Load checks the keys too; this catches a Config built by hand.
	if len(cfg.Auth.JWTKey) != ed25519.PrivateKeySize || len(cfg.Auth.OTPKey) < 32 {
		return nil, errors.New("auth keys missing from config; run make run, which adds them to .env")
	}
	if len(cfg.Staff.Key) < 32 || cfg.Staff.ConsoleOrigin == "" {
		return nil, errors.New("staff key or console origin missing from config; run make run, which adds the key to .env")
	}
	o := options{sms: sms.LogSender{Log: log}, email: email.LogSender{Log: log}, hasher: member.Production}
	for _, opt := range opts {
		opt(&o)
	}
	// A log sender writes every OTP code to the log.
	_, smsLogs := o.sms.(sms.LogSender)
	_, emailLogs := o.email.(email.LogSender)
	if cfg.Env == "prod" && (smsLogs || emailLogs) {
		return nil, errors.New("APP_ENV=prod with a log sender for SMS or email; configure real senders")
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	a := &App{router: httpx.NewRouter(), pool: pool, adminURL: cfg.Restate.AdminURL}
	defer func() {
		if err != nil {
			err = errors.Join(err, a.Close(context.Background()))
		}
	}()

	if err = checkMigrations(ctx, pool); err != nil {
		return nil, err
	}
	doc, err := api.Load()
	if err != nil {
		return nil, fmt.Errorf("load api spec: %w", err)
	}
	validate, err := httpx.ValidateRequests(doc, log)
	if err != nil {
		return nil, fmt.Errorf("build request validator: %w", err)
	}

	a.router.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.Respond(w, http.StatusOK, status{"ok"})
	})
	a.router.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if notReady := a.ready(r.Context()); notReady != nil {
			log.WarnContext(r.Context(), "not ready", "err", notReady)
			httpx.Respond(w, http.StatusServiceUnavailable, status{"unavailable"})
			return
		}
		httpx.Respond(w, http.StatusOK, status{"ok"})
	})
	defs, err := mountModules(a.router, pool, cfg, o, log)
	if err != nil {
		return nil, err
	}
	if a.endpoint, err = startRestate(ctx, cfg.Restate, log, defs); err != nil {
		return nil, err
	}

	// Trace is outermost so a panic's 500 still carries a trace id.
	cors := httpx.CORS(cfg.Staff.ConsoleOrigin, "/v1/staff/")
	clientIP := httpx.ResolveClientIP(cfg.TrustedProxies)
	a.Handler = httpx.Trace(httpx.Recover(log)(httpx.AccessLog(log)(clientIP(cors(validate(a.router))))))
	return a, nil
}

type status struct {
	Status string `json:"status"`
}

func checkMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	pending, err := postgres.Pending(ctx, pool, migrations.FS)
	if err != nil {
		return fmt.Errorf("check migrations: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	names := make([]string, len(pending))
	for i, m := range pending {
		names[i] = m.File()
	}
	return fmt.Errorf("migrations pending: %s; run go run ./cmd/migrate or make run", strings.Join(names, ", "))
}

// mountModules mounts every module and returns their Restate services. A
// module mounts after the modules whose ports it takes; protect comes first
// because it needs only the auth keys.
func mountModules(router *httpx.Router, pool *pgxpool.Pool, cfg config.Config, o options, log *slog.Logger) ([]restate.ServiceDefinition, error) {
	d := module.Deps{
		Router: router, Pool: pool,
		Ingress: ingress.NewClient(cfg.Restate.IngressURL), ServicePrefix: cfg.Restate.ServicePrefix,
		Now: time.Now, Log: log,
	}
	protect := auth.Protect(d, cfg.Auth)
	accounts := account.Mount(d, protect)
	authm := auth.Mount(d, auth.Settings{Keys: cfg.Auth, SMS: o.sms, Email: o.email}, accounts)
	staffKeys, err := staff.NewKeys(cfg.Staff, o.hasher)
	if err != nil {
		return nil, fmt.Errorf("staff keys: %w", err)
	}
	staff.Mount(d, staffKeys, accounts, authm.Client)
	return authm.Services, nil
}

func startRestate(ctx context.Context, cfg config.RestateConfig, log *slog.Logger, defs []restate.ServiceDefinition) (*restatex.Endpoint, error) {
	ep, err := restatex.Serve(ctx, cfg.ListenAddr, cfg.IdentityKey, defs...)
	if err != nil {
		return nil, fmt.Errorf("restate endpoint: %w", err)
	}
	url := cfg.EndpointURL
	if url == "" {
		url = ep.URL()
	}
	if err := register(ctx, cfg.AdminURL, url, log); err != nil {
		return nil, errors.Join(err, ep.Close(context.Background()))
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
			return fmt.Errorf("register restate endpoint: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("register restate endpoint: %w", err)
}

func (a *App) ready(ctx context.Context) error {
	if err := a.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	if err := restatex.Healthy(ctx, a.adminURL); err != nil {
		return fmt.Errorf("check restate: %w", err)
	}
	return nil
}

// Close stops the Restate endpoint and closes the pool.
func (a *App) Close(ctx context.Context) error {
	var err error
	if a.endpoint != nil {
		if cerr := a.endpoint.Close(ctx); cerr != nil {
			err = fmt.Errorf("stop restate endpoint: %w", cerr)
		}
	}
	a.pool.Close()
	return err
}
