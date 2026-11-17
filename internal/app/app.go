// Package app builds every dependency of the server by hand. cmd/server and
// the e2e tests both start the server through New.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/api"
	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/agent"
	"github.com/shopnexus/server2/internal/assist"
	"github.com/shopnexus/server2/internal/catalog"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/chat"
	"github.com/shopnexus/server2/internal/console"
	consoledomain "github.com/shopnexus/server2/internal/console/domain"
	consolesvc "github.com/shopnexus/server2/internal/console/service"
	"github.com/shopnexus/server2/internal/deal"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/discovery"
	"github.com/shopnexus/server2/internal/erasure"
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
	"github.com/shopnexus/server2/internal/provider/carrier"
	"github.com/shopnexus/server2/internal/provider/chotot"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/firebase"
	"github.com/shopnexus/server2/internal/provider/frames"
	"github.com/shopnexus/server2/internal/provider/google"
	"github.com/shopnexus/server2/internal/provider/llm"
	"github.com/shopnexus/server2/internal/provider/push"
	"github.com/shopnexus/server2/internal/provider/stt"
	"github.com/shopnexus/server2/internal/screen"
	"github.com/shopnexus/server2/internal/shipping"
	shippingsvc "github.com/shopnexus/server2/internal/shipping/service"
	"github.com/shopnexus/server2/internal/support"
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
	hub      *stream.Hub
	agent    *agent.Mounted
	// stop cancels the event bus, the stream hub and the agent's MCP
	// server, which run in bg.
	stop context.CancelFunc
	bg   sync.WaitGroup
}

// Option changes how New builds the app.
type Option func(*options)

type options struct {
	push   push.Sender
	email  email.Sender
	hasher consoledomain.Hasher
}

// WithPushSender replaces the sender the configuration picks; e2e tests
// read pushes from theirs.
func WithPushSender(s push.Sender) Option { return func(o *options) { o.push = s } }

// WithEmailSender replaces the sender EMAIL_SENDER picks; e2e tests read
// codes from theirs.
func WithEmailSender(s email.Sender) Option { return func(o *options) { o.email = s } }

// WithPasswordHasher sets the argon2id parameters of new member passwords;
// tests pass consoledomain.Fast.
func WithPasswordHasher(h consoledomain.Hasher) Option { return func(o *options) { o.hasher = h } }

// AgentMCPAddr is where the agent's MCP server listens, for tests that
// set AGENT_MCP_ADDR to port 0.
func (a *App) AgentMCPAddr() net.Addr { return a.agent.Addr() }

// Routes lists every registered route, for the route-versus-spec test.
func (a *App) Routes() []httpx.Route { return a.router.Routes() }

// New builds the server for cfg and starts its background work. It refuses to
// start while migrations are pending, so the server never runs against an
// older schema.
// carrierTimeout bounds a call to GHTK or GHN.
const carrierTimeout = 20 * time.Second

func New(ctx context.Context, cfg config.Config, log *slog.Logger, opts ...Option) (_ *App, err error) {
	o := options{hasher: consoledomain.Production}
	for _, opt := range opts {
		opt(&o)
	}
	// config.Load checks the key too; this catches a Config built by hand.
	if len(cfg.Auth.OTPKey) < 32 {
		return nil, errors.New("OTP_KEY missing from config; make run adds it to .env")
	}
	if cfg.Env == config.EnvProd && cfg.Firebase.TestNumbers {
		return nil, errors.New("APP_ENV=prod with PNV_TEST_NUMBERS=true; test numbers would sign in")
	}
	sender := o.push
	switch {
	case sender != nil:
	case len(cfg.Push.Credentials) > 0:
		// The sender keeps this context to refresh its token, so it is not ctx.
		fcm, fcmErr := push.NewFCM(context.Background(), cfg.Push.Credentials, cfg.Firebase.ProjectID)
		if fcmErr != nil {
			return nil, fmt.Errorf("push: %w", fcmErr)
		}
		sender = fcm
	case cfg.Env == config.EnvProd:
		return nil, errors.New("APP_ENV=prod with FCM_CREDENTIALS empty; pushes would only be logged")
	default:
		sender = push.LogSender{Log: log}
	}
	mailer := o.email
	switch {
	case mailer != nil:
	case cfg.Email.Sender == "resend":
		mailer = email.NewResend(cfg.Email.ResendAPIKey, cfg.Email.From)
	case cfg.Env == config.EnvProd:
		return nil, errors.New("APP_ENV=prod with EMAIL_SENDER=log; codes would only be logged")
	default:
		mailer = email.LogSender{Log: log}
	}
	// carriers holds one client per carrier, and the secret in each
	// carrier's webhook path; none has no client and no webhook.
	carriers := shipping.Settings{Key: cfg.Carrier.TokenKey}
	switch cfg.Carrier.Mode {
	case "fake":
		if cfg.Env == config.EnvProd {
			return nil, errors.New("APP_ENV=prod with CARRIER=fake; anyone could move shipments through the dev endpoint")
		}
		carriers.Clients = map[carrier.Carrier]carrier.Client{
			carrier.GHTK: carrier.Fake{Carrier: carrier.GHTK}, carrier.GHN: carrier.Fake{Carrier: carrier.GHN},
		}
		// The fake's webhooks are open to anyone who knows "dev", as its dev
		// endpoint is.
		carriers.WebhookSecrets = map[carrier.Carrier]string{carrier.GHTK: "dev", carrier.GHN: "dev"}
	case "live":
		web := &http.Client{Timeout: carrierTimeout}
		c := cfg.Carrier
		carriers.Clients = map[carrier.Carrier]carrier.Client{
			carrier.GHTK: carrier.GHTKClient{BaseURL: c.GHTKURL, PartnerToken: c.GHTKPartner, ClientSource: c.GHTKSource, HTTP: web},
			carrier.GHN:  carrier.GHNClient{BaseURL: c.GHNURL, HTTP: web},
		}
		carriers.WebhookSecrets = map[carrier.Carrier]string{carrier.GHTK: c.GHTKWebhook, carrier.GHN: c.GHNWebhook}
	case "none":
	default:
		return nil, fmt.Errorf("CARRIER=%q: want fake, none or live", cfg.Carrier.Mode)
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	runCtx, stop := context.WithCancel(context.Background())
	a := &App{router: httpx.NewRouter(), pool: pool, adminURL: cfg.Restate.AdminURL, hub: stream.NewHub(stream.PGBell{DB: pool, Log: log}, stream.PGLog{DB: pool}, time.Now, log), stop: stop}
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
	defs, agentm, err := mountModules(a.router, pool, in, bus, a.hub, cfg, o.hasher, sender, mailer, carriers, log)
	if err != nil {
		return nil, err
	}
	// The MCP listener is open; Run serves it until Close.
	a.agent = agentm
	a.bg.Go(func() {
		if runErr := agentm.Run(runCtx); runErr != nil {
			log.ErrorContext(runCtx, "agent MCP server stopped", "err", runErr)
		}
	})
	defs = append(defs, bus.Definition())
	if err = bus.Check(defs); err != nil {
		return nil, fmt.Errorf("check event subscriptions: %w", err)
	}
	// The validator matches only the mounted routes, so a dev route this
	// server does not mount answers as an unknown path.
	validate, err := httpx.ValidateRequests(doc, a.router.Routes(), log)
	if err != nil {
		return nil, fmt.Errorf("build request validator: %w", err)
	}
	if a.endpoint, err = startRestate(ctx, cfg.Restate, log, defs); err != nil {
		return nil, err
	}
	a.bg.Go(func() {
		if err := bus.Run(runCtx); err != nil {
			log.ErrorContext(runCtx, "event bus stopped", "err", err)
		}
	})
	a.bg.Go(func() {
		if err := a.hub.Run(runCtx); err != nil {
			log.ErrorContext(runCtx, "stream hub stopped", "err", err)
		}
	})

	// Trace is outermost, so a panic's 500 carries a trace id and gets its
	// access log line.
	cors := httpx.CORS(cfg.Console.Origin, "/v1/console/")
	docsCORS := httpx.DocsCORS(cfg.DocsOrigins)
	clientIP := httpx.ResolveClientIP(cfg.TrustedProxies)
	a.Handler = httpx.Trace(log)(httpx.Recover(log)(clientIP(cors(docsCORS(validate(a.router))))))
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

// mountModules mounts every module and returns their Restate services and
// the agent module, whose MCP server app runs. A module mounts after the
// modules whose services it takes; account's guards come first because they
// need only the auth keys.
func mountModules(router *httpx.Router, pool *pgxpool.Pool, in *ingress.Client, bus *events.Bus, hub *stream.Hub,
	cfg config.Config, hasher consoledomain.Hasher, sender push.Sender, mailer email.Sender, carriers shipping.Settings,
	log *slog.Logger,
) ([]restate.ServiceDefinition, *agent.Mounted, error) {
	d := module.Deps{
		Router: router, Pool: pool, Ingress: in, ServicePrefix: cfg.Restate.ServicePrefix,
		Events: bus, Now: time.Now, Log: log,
	}
	d.Protect, d.Identify = account.Guards(d, cfg.Auth)
	store, err := objectstore.New(cfg.ObjectStore)
	if err != nil {
		return nil, nil, fmt.Errorf("object store: %w", err)
	}
	mediam := media.Mount(d, media.Settings{Store: store})
	accountm := account.Mount(d,
		account.Settings{
			Keys: cfg.Auth, Phones: firebase.NewPhoneVerifier(cfg.Firebase, time.Now, log),
			Google: google.NewVerifier(cfg.Google, time.Now, log), Email: mailer,
		}, mediam.Service)
	accounts := accountm.Service
	consoleKeys, err := consolesvc.NewKeys(cfg.Console, hasher)
	if err != nil {
		return nil, nil, fmt.Errorf("console keys: %w", err)
	}
	// An empty model turns its kind off: the modules get nil and write and
	// read no vectors of that kind.
	embedder := func(base, model, key string, dims int) embed.Model {
		if model == "" {
			return nil
		}
		return embed.OpenAI{BaseURL: base, APIKey: key, Model: model, Dims: dims, HTTP: &http.Client{Timeout: cfg.Embed.Timeout}}
	}
	catalogm := catalog.Mount(d, catalog.Settings{
		Text:        embedder(cfg.Embed.TextBaseURL, cfg.Embed.TextModel, cfg.Embed.IndexAPIKey, catalogdomain.TextDims),
		Image:       embedder(cfg.Embed.ImageBaseURL, cfg.Embed.ImageModel, cfg.Embed.IndexAPIKey, catalogdomain.PhotoDims),
		TextVersion: cfg.Embed.TextVersion, ImageVersion: cfg.Embed.ImageVersion, EmbedSlots: cfg.Embed.Slots,
	}, accounts, mediam.Service)
	// shipping reaches deal through these functions, filled once deal is
	// mounted, since deal makes its shipments through shipping.
	var deals dealsvc.Service
	carriers.Deals = shippingsvc.Deals{
		ShipmentUpdated: func(ctx context.Context, code, status string) error { return deals.ShipmentUpdated(ctx, code, status) },
		OpenShipmentsAt: func(ctx context.Context, seller uuid.UUID, c string) (bool, error) {
			return deals.OpenShipmentsAt(ctx, seller, c)
		},
		QuoteFacts: func(ctx context.Context, caller, order, address uuid.UUID) (shippingsvc.QuoteFacts, error) {
			to, from, price, err := deals.QuoteFacts(ctx, caller, order, address)
			if err != nil {
				return shippingsvc.QuoteFacts{}, fmt.Errorf("read the order to price: %w", err)
			}
			return shippingsvc.QuoteFacts{Recipient: to, Pickup: from, Price: price}, nil
		},
		NotFound: dealdomain.ErrShipmentNotFound,
	}
	shippingm := shipping.Mount(d, carriers, accounts)
	// A deal opens the pair's conversation and writes its moves there, so
	// chat mounts first.
	chatm := chat.Mount(d, accounts, catalogm.Service, mediam.Service)
	dealm := deal.Mount(d, deal.Settings{Shipping: shippingm.Service, FakeCarrier: cfg.Carrier.Mode == "fake", PollEvery: cfg.Carrier.PollEvery},
		accounts, catalogm.Service, chatm.Service)
	deals = dealm.Service
	erasure.Mount(d, accounts, dealm.Service)
	supportm := support.Mount(d, accounts, chatm.Service, dealm.Service, catalogm.Service)
	trustm := trust.Mount(d, trust.Settings{
		Classifier: trust.LLMClassifier(
			llm.OpenAI{BaseURL: cfg.Trust.LLMBaseURL, APIKey: cfg.Trust.LLMAPIKey, Model: cfg.Trust.LLMModel, HTTP: &http.Client{}},
			cfg.Trust.LLMModel, cfg.Trust.CallTimeout, log),
		TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity,
	}, accounts, catalogm.Service, dealm.Service, chatm.Service)
	consolem := console.Mount(d, console.Settings{Keys: consoleKeys, Config: cfg.Console}, accounts, supportm.Service, trustm.Service,
		catalogm.Service)
	model := llm.OpenAI{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model, HTTP: &http.Client{}}
	discoverym := discovery.Mount(d, discovery.Settings{
		Model:       model,
		Text:        embedder(cfg.Embed.TextBaseURL, cfg.Embed.TextModel, cfg.Embed.SearchAPIKey, catalogdomain.TextDims),
		Image:       embedder(cfg.Embed.ImageBaseURL, cfg.Embed.ImageModel, cfg.Embed.SearchAPIKey, catalogdomain.PhotoDims),
		TextVersion: cfg.Embed.TextVersion, ImageVersion: cfg.Embed.ImageVersion,
		TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity,
	}, catalogm.Service, mediam.Service, accounts)
	assistm := assist.Mount(d, assist.Settings{
		Frames: frames.Client{BaseURL: cfg.Assist.FramesBaseURL, HTTP: &http.Client{}},
		STT: stt.OpenAI{
			BaseURL: cfg.Assist.STTBaseURL, APIKey: cfg.Assist.STTAPIKey, Model: cfg.Assist.STTModel, HTTP: &http.Client{},
		},
		Model: llm.OpenAI{
			BaseURL: cfg.Assist.LLMBaseURL, APIKey: cfg.Assist.LLMAPIKey, Model: cfg.Assist.LLMModel, HTTP: &http.Client{},
		},
		Text:    embedder(cfg.Embed.TextBaseURL, cfg.Embed.TextModel, cfg.Embed.SearchAPIKey, catalogdomain.TextDims),
		Image:   embedder(cfg.Embed.ImageBaseURL, cfg.Embed.ImageModel, cfg.Embed.SearchAPIKey, catalogdomain.PhotoDims),
		TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity, CallTimeout: cfg.Assist.CallTimeout,
		Source: chotot.Client{
			GatewayURL: cfg.Assist.ChototGatewayURL, CDNURL: cfg.Assist.ChototCDNURL, HTTP: &http.Client{},
		},
	}, accounts, mediam.Service, catalogm.Service, discoverym.Service)
	updates := stream.PGLog{DB: pool}
	router.Handle("GET /v1/ws", d.Protect(stream.Socket{
		Hub: hub, Updates: updates, Access: socketAccess{accounts: accounts, chats: chatm.Service},
		Verify: account.TokenVerifier(cfg.Auth), Tick: time.Minute, Now: time.Now, Log: log,
	}))
	router.Handle("GET /v1/updates", d.Protect(stream.UpdatesHandler(updates, log)))
	if cfg.Env != config.EnvProd {
		router.Handle("POST /v1/dev/updates", d.Protect(stream.DevUpdateHandler(updates, log)))
	}
	notificationm := notification.Mount(d, notification.Settings{Push: sender},
		accounts, catalogm.Service, dealm.Service, trustm.Service, chatm.Service, discoverym.Service, supportm.Service)
	screen.Mount(d, accounts, catalogm.Service, dealm.Service, trustm.Service, discoverym.Service, chatm.Service, notificationm.Service,
		supportm.Service, shippingm.Service)
	agentm, agentErr := agent.Mount(d, agent.Settings{
		Config: cfg.Agent, TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity,
	}, consolem.Moderator, consolem.Service, supportm.Service, accounts, catalogm.Service, dealm.Service, shippingm.Service)
	if agentErr != nil {
		return nil, nil, fmt.Errorf("agent: %w", agentErr)
	}
	return slices.Concat(accountm.Services, mediam.Services, catalogm.Services, shippingm.Services, dealm.Services, discoverym.Services,
		chatm.Services, supportm.Services, trustm.Services, notificationm.Services, assistm.Services), agentm, nil
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

// CloseStreams ends every open WebSocket with 1001, so a server shutdown
// does not wait for them; the server's RegisterOnShutdown calls it.
func (a *App) CloseStreams() { a.hub.Close() }

// Close ends the streams, stops the event bus, the stream hub, the agent's
// MCP server and the Restate endpoint, then closes the pool.
func (a *App) Close(ctx context.Context) error {
	a.CloseStreams()
	a.stop()
	a.bg.Wait()
	var err error
	if a.endpoint != nil {
		if cerr := a.endpoint.Close(ctx); cerr != nil {
			err = fmt.Errorf("stop restate endpoint: %w", cerr)
		}
	}
	a.pool.Close()
	return err
}
