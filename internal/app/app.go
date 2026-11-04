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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/catalog"
	"github.com/shopnexus/server2/internal/chat"
	"github.com/shopnexus/server2/internal/deal"
	"github.com/shopnexus/server2/internal/discovery"
	"github.com/shopnexus/server2/internal/media"
	"github.com/shopnexus/server2/internal/notification"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/platform/stream"
	"github.com/shopnexus/server2/internal/provider/firebase"
	"github.com/shopnexus/server2/internal/provider/llm"
	"github.com/shopnexus/server2/internal/provider/push"
	"github.com/shopnexus/server2/internal/screen"
	"github.com/shopnexus/server2/internal/staff"
	staffdomain "github.com/shopnexus/server2/internal/staff/domain"
	staffsvc "github.com/shopnexus/server2/internal/staff/service"
	"github.com/shopnexus/server2/internal/trust"
	"github.com/shopnexus/server2/migrations"
)

// App is the assembled server.
type App struct {
	Handler  http.Handler
	router   *httpx.Router
	pool     *pgxpool.Pool
	endpoint *restatex.Endpoint
	adminURL string
	// stopEvents cancels the event bus and the stream hub; eventsDone
	// closes when both returned.
	stopEvents context.CancelFunc
	eventsDone chan struct{}
	hub        *stream.Hub
}

// Option changes how New builds the app.
type Option func(*options)

type options struct {
	push   push.Sender
	hasher staffdomain.Hasher
}

// WithPushSender replaces the sender the configuration picks; e2e tests
// read pushes from theirs.
func WithPushSender(s push.Sender) Option { return func(o *options) { o.push = s } }

// WithPasswordHasher sets the argon2id parameters of new staff passwords;
// tests pass staffdomain.Fast.
func WithPasswordHasher(h staffdomain.Hasher) Option { return func(o *options) { o.hasher = h } }

// Routes lists every registered route, for the route-versus-spec test.
func (a *App) Routes() []httpx.Route { return a.router.Routes() }

// New builds the server for cfg and starts its background work. It refuses to
// start while migrations are pending, so the server never runs against an
// older schema.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, opts ...Option) (_ *App, err error) {
	// config.Load checks the keys too; this catches a Config built by hand.
	if len(cfg.Auth.JWTKey) != ed25519.PrivateKeySize {
		return nil, errors.New("auth keys missing from config; run make run, which adds them to .env")
	}
	if len(cfg.Staff.Key) < 32 || cfg.Staff.ConsoleOrigin == "" {
		return nil, errors.New("staff key or console origin missing from config; run make run, which adds the key to .env")
	}
	o := options{hasher: staffdomain.Production}
	for _, opt := range opts {
		opt(&o)
	}
	if _, pushLogs := o.push.(push.LogSender); cfg.Env == config.EnvProd && (pushLogs || o.push == nil && len(cfg.Push.Credentials) == 0) {
		return nil, errors.New("APP_ENV=prod with FCM_CREDENTIALS empty; pushes would only be logged")
	}
	if cfg.Env == config.EnvProd && cfg.Firebase.TestNumbers {
		return nil, errors.New("APP_ENV=prod with PNV_TEST_NUMBERS=true; test numbers would sign in")
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
	in := ingress.NewClient(cfg.Restate.IngressURL)
	bus := events.NewBus(pool, in, cfg.Restate.ServicePrefix, time.Now, log)
	hub := stream.NewHub(pool, log)
	a.hub = hub
	defs, err := mountModules(a.router, pool, in, bus, hub, cfg, o, log)
	if err != nil {
		return nil, err
	}
	defs = append(defs, bus.Definition())
	if err = bus.Check(defs); err != nil {
		return nil, fmt.Errorf("check event subscriptions: %w", err)
	}
	if a.endpoint, err = startRestate(ctx, cfg.Restate, log, defs); err != nil {
		return nil, err
	}
	runCtx, stop := context.WithCancel(context.Background())
	a.stopEvents, a.eventsDone = stop, make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Go(func() {
			if err := bus.Run(runCtx); err != nil {
				log.ErrorContext(runCtx, "event bus stopped", "err", err)
			}
		})
		wg.Go(func() {
			if err := hub.Run(runCtx); err != nil {
				log.ErrorContext(runCtx, "stream hub stopped", "err", err)
			}
		})
		wg.Wait()
		close(a.eventsDone)
	}()

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
// module mounts after the modules whose services it takes; protect comes first
// because it needs only the auth keys.
func mountModules(router *httpx.Router, pool *pgxpool.Pool, in *ingress.Client, bus *events.Bus, hub *stream.Hub,
	cfg config.Config, o options, log *slog.Logger,
) ([]restate.ServiceDefinition, error) {
	d := module.Deps{
		Router: router, Pool: pool, Ingress: in, ServicePrefix: cfg.Restate.ServicePrefix,
		Events: bus, Now: time.Now, Log: log,
	}
	protect := auth.Protect(d, cfg.Auth)
	router.Handle("GET /v1/stream", protect(hub.Handler(log)))
	store, err := objectstore.New(cfg.ObjectStore)
	if err != nil {
		return nil, fmt.Errorf("object store: %w", err)
	}
	mediam := media.Mount(d, store, protect)
	accounts := account.Mount(d, protect, mediam.Service)
	authm := auth.Mount(d, auth.Settings{Keys: cfg.Auth, Phones: firebase.NewPhoneVerifier(cfg.Firebase, time.Now, log)}, accounts)
	staffKeys, err := staffsvc.NewKeys(cfg.Staff, o.hasher)
	if err != nil {
		return nil, fmt.Errorf("staff keys: %w", err)
	}
	staff.Mount(d, staffKeys, accounts, authm.Service)
	catalogm := catalog.Mount(d, catalog.Guards{Protect: protect, Identify: auth.Identify(d, cfg.Auth)},
		accounts, mediam.Service)
	dealm := deal.Mount(d, protect, accounts, catalogm.Service)
	trustm := trust.Mount(d, protect, accounts, catalogm.Service, dealm.Service)
	model := llm.OpenAI{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model, HTTP: &http.Client{}}
	saves := discovery.Mount(d, discovery.Guards{Protect: protect, Identify: auth.Identify(d, cfg.Auth)},
		catalogm.Service, model)
	chatm := chat.Mount(d, protect, accounts, catalogm.Service, mediam.Service)
	sender := o.push
	if sender == nil {
		sender = push.LogSender{Log: log}
		if len(cfg.Push.Credentials) > 0 {
			fcm, fcmErr := push.NewFCM(context.Background(), cfg.Push.Credentials, cfg.Firebase.ProjectID)
			if fcmErr != nil {
				return nil, fmt.Errorf("push: %w", fcmErr)
			}
			sender = fcm
		}
	}
	notificationm := notification.Mount(d, protect, notification.Sources{
		Accounts: accounts, Auth: authm.Service, Catalog: catalogm.Service, Deals: dealm.Service, Chats: chatm.Service, Trust: trustm,
	}, sender)
	screen.Mount(d, screen.Guards{Protect: protect, Identify: auth.Identify(d, cfg.Auth)},
		accounts, catalogm.Service, dealm.Service, trustm, saves, chatm.Service, notificationm.Service)
	return slices.Concat(authm.Services, mediam.Services, catalogm.Services, dealm.Services, chatm.Services,
		notificationm.Services), nil
}

func startRestate(ctx context.Context, cfg config.RestateConfig, log *slog.Logger, defs []restate.ServiceDefinition) (*restatex.Endpoint, error) {
	ep, err := restatex.Serve(ctx, cfg.ListenAddr, cfg.IdentityKey, defs...)
	if err != nil {
		return nil, fmt.Errorf("restate endpoint: %w", err)
	}
	if !cfg.Register {
		log.InfoContext(ctx, "restate endpoint served; registration is left to restate-operator", "addr", cfg.ListenAddr)
		return ep, nil
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

// CloseStreams ends every open /v1/stream, so a server shutdown does not
// wait for them; the server's RegisterOnShutdown calls it.
func (a *App) CloseStreams() {
	if a.hub != nil {
		a.hub.Close()
	}
}

// Close ends the streams, stops the event bus, the stream hub and the
// Restate endpoint, then closes the pool.
func (a *App) Close(ctx context.Context) error {
	a.CloseStreams()
	if a.stopEvents != nil {
		a.stopEvents()
		<-a.eventsDone
	}
	var err error
	if a.endpoint != nil {
		if cerr := a.endpoint.Close(ctx); cerr != nil {
			err = fmt.Errorf("stop restate endpoint: %w", cerr)
		}
	}
	a.pool.Close()
	return err
}
