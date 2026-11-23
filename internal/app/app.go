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

	"github.com/shopnexus/server/api"
	"github.com/shopnexus/server/internal/account"
	"github.com/shopnexus/server/internal/agent"
	"github.com/shopnexus/server/internal/ai"
	aisvc "github.com/shopnexus/server/internal/ai/service"
	"github.com/shopnexus/server/internal/assist"
	"github.com/shopnexus/server/internal/beacon"
	"github.com/shopnexus/server/internal/catalog"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chat"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/console"
	consoledomain "github.com/shopnexus/server/internal/console/domain"
	consolesvc "github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/deal"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/discovery"
	"github.com/shopnexus/server/internal/erasure"
	"github.com/shopnexus/server/internal/media"
	"github.com/shopnexus/server/internal/notification"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/geoip"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/relay"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/platform/stream"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/provider/chotot"
	"github.com/shopnexus/server/internal/provider/email"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/firebase"
	"github.com/shopnexus/server/internal/provider/google"
	"github.com/shopnexus/server/internal/provider/indexnow"
	"github.com/shopnexus/server/internal/provider/llm"
	"github.com/shopnexus/server/internal/provider/push"
	"github.com/shopnexus/server/internal/screen"
	"github.com/shopnexus/server/internal/shipping"
	shippingsvc "github.com/shopnexus/server/internal/shipping/service"
	"github.com/shopnexus/server/internal/support"
	"github.com/shopnexus/server/internal/trust"
	"github.com/shopnexus/server/migrations"
)

// App is the assembled server.
type App struct {
	Handler  http.Handler
	router   *httpx.Router
	pool     *pgxpool.Pool
	endpoint *restatex.Endpoint
	adminURL string
	hub      *stream.Hub
	relay    *relay.Relay
	agent    *agent.Mounted
	// closeAI releases module ai's models; nil when none are loaded.
	closeAI func()
	// geo names the access log's countries; nil without GEOIP_DB.
	geo *geoip.DB
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
	ai     *ai.Engines
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

// WithAIEngines gives module ai these engines in place of the models
// AI_* names, which are then not loaded; tests pass fakes.
func WithAIEngines(e ai.Engines) Option { return func(o *options) { o.ai = &e } }

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
	if cfg.Role == config.RoleAI {
		return newAI(ctx, cfg, log, o)
	}
	if cfg.Env == config.EnvProd && cfg.Firebase.TestNumbers {
		return nil, errors.New("APP_ENV=prod with PNV_TEST_NUMBERS=true; test numbers would sign in")
	}
	sender := o.push
	switch {
	case sender != nil:
	case len(cfg.Push.Credentials) > 0:
		// The sender keeps this context to refresh its token, so it is not ctx.
		fcm, fcmErr := push.NewFCM(context.Background(), httpx.NewClient(0), cfg.Push.Credentials, cfg.Firebase.ProjectID)
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
		mailer = email.NewResend(httpx.NewClient(10*time.Second), cfg.Email.ResendAPIKey, cfg.Email.From)
	case cfg.Env == config.EnvProd:
		return nil, errors.New("APP_ENV=prod with EMAIL_SENDER=log; mail would only be logged")
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
		web := httpx.NewClient(carrierTimeout)
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
	a := &App{router: httpx.NewRouter(), pool: pool, adminURL: cfg.Restate.AdminURL, hub: stream.NewHub(stream.PGBell{DB: pool, Log: log}, stream.PGLog{DB: pool}, time.Now, log), relay: relay.New(pool, log), stop: stop}
	defer func() {
		if err != nil {
			err = errors.Join(err, a.Close(context.Background()))
		}
	}()

	if err = checkMigrations(ctx, pool); err != nil {
		return nil, err
	}
	if a.geo, err = geoip.Open(cfg.GeoIPDB); err != nil {
		return nil, fmt.Errorf("GEOIP_DB: %w", err)
	}
	doc, err := api.Load()
	if err != nil {
		return nil, fmt.Errorf("load api spec: %w", err)
	}
	// SERVER_ROLE=api loads no model; all serves ai in this process.
	var engines *ai.Engines
	if cfg.Role.ServesAI() {
		if engines, err = a.loadAI(cfg.AI, o); err != nil {
			return nil, err
		}
	}

	a.health(log)
	in := ingress.NewClient(cfg.Restate.IngressURL, ingress.WithHttpClient(httpx.NewClient(0)))
	bus := events.NewBus(pool, in, cfg.Restate.ServicePrefix, time.Now, log)
	defs, agentm, err := mountModules(a.router, pool, in, bus, a.hub, a.relay, cfg, o.hasher, sender, mailer, carriers, engines, log)
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
	limits, err := httpx.NewLimits(doc, time.Now, log)
	if err != nil {
		return nil, fmt.Errorf("read rate limits: %w", err)
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
	a.bg.Go(func() { a.relay.Run(runCtx) })

	// Trace is outermost, so a panic's 500 carries a trace id and gets its
	// access log line.
	cors := httpx.CORS(cfg.Console.Origin, "/v1/console/")
	docsCORS := httpx.DocsCORS(cfg.DocsOrigins)
	clientIP := httpx.ResolveClientIP(cfg.TrustedProxies)
	visitors := httpx.Visitors(a.geo, cfg.TrustedProxies, time.Now)
	a.Handler = httpx.Trace(log)(httpx.Recover(log)(clientIP(visitors(cors(docsCORS(limits.Middleware(a.router.Route)(validate(a.router))))))))
	return a, nil
}

type status struct {
	Status string `json:"status"`
}

// newAI builds a process of SERVER_ROLE=ai: module ai's models and its
// Restate service, and on HTTP only /healthz and /readyz. It opens no
// database and runs no other module.
func newAI(ctx context.Context, cfg config.Config, log *slog.Logger, o options) (_ *App, err error) {
	a := &App{router: httpx.NewRouter(), adminURL: cfg.Restate.AdminURL, stop: func() {}}
	defer func() {
		if err != nil {
			err = errors.Join(err, a.Close(context.Background()))
		}
	}()
	engines, err := a.loadAI(cfg.AI, o)
	if err != nil {
		return nil, err
	}
	store, err := objectstore.New(cfg.ObjectStore)
	if err != nil {
		return nil, fmt.Errorf("object store: %w", err)
	}
	d := module.Deps{
		Router: a.router, Ingress: ingress.NewClient(cfg.Restate.IngressURL, ingress.WithHttpClient(httpx.NewClient(0))), ServicePrefix: cfg.Restate.ServicePrefix,
		Now: time.Now, Log: log,
	}
	aim := ai.Mount(d, ai.Settings{Engines: *engines, Objects: store, Config: cfg.AI})
	a.health(log)
	if a.endpoint, err = startRestate(ctx, cfg.Restate, log, aim.Services); err != nil {
		return nil, err
	}
	a.Handler = httpx.Trace(log)(httpx.Recover(log)(a.router))
	return a, nil
}

// loadAI loads module ai's models, or takes the engines of WithAIEngines,
// and keeps what releases them for Close.
func (a *App) loadAI(c config.AIConfig, o options) (*ai.Engines, error) {
	if o.ai != nil {
		return o.ai, nil
	}
	e, err := ai.Load(c)
	if err != nil {
		return nil, fmt.Errorf("module ai: %w", err)
	}
	a.closeAI = e.Close
	return &e, nil
}

// health registers the probes: /healthz says the process runs, /readyz
// that Postgres (when the process uses it) and Restate answer.
func (a *App) health(log *slog.Logger) {
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
// need only the auth keys. Module ai's service is among them when engines
// is not nil; the other modules call ai through Restate either way.
func mountModules(router *httpx.Router, pool *pgxpool.Pool, in *ingress.Client, bus *events.Bus, hub *stream.Hub, rel *relay.Relay,
	cfg config.Config, hasher consoledomain.Hasher, sender push.Sender, mailer email.Sender, carriers shipping.Settings,
	engines *ai.Engines, log *slog.Logger,
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
	// The website's pages are under its first origin, which IndexNow is
	// told of and mail links to.
	var website string
	if len(cfg.ObjectStore.UploadOrigins) > 0 {
		website = cfg.ObjectStore.UploadOrigins[0]
	}
	accountm := account.Mount(d,
		account.Settings{
			Keys: cfg.Auth, Phones: firebase.NewPhoneVerifier(cfg.Firebase, time.Now, log),
			Google: google.NewVerifier(cfg.Google, time.Now, log), Email: mailer, Site: website,
		}, mediam.Service)
	accounts := accountm.Service
	consoleKeys, err := consolesvc.NewKeys(cfg.Console, hasher)
	if err != nil {
		return nil, nil, fmt.Errorf("console keys: %w", err)
	}
	aiClient := ai.Client(d)
	var aiServices []restate.ServiceDefinition
	if engines != nil {
		aiServices = ai.Mount(d, ai.Settings{Engines: *engines, Objects: store, Config: cfg.AI}).Services
	}
	// An empty model turns its kind off: the modules get nil and write and
	// read no vectors of that kind.
	embedder := func(key string) embed.Model {
		if cfg.Embed.TextModel == "" {
			return nil
		}
		return embed.OpenAI{
			BaseURL: cfg.Embed.TextBaseURL, APIKey: key, Model: cfg.Embed.TextModel, Dims: catalogdomain.TextDims,
			HTTP: httpx.NewClient(cfg.Embed.Timeout),
		}
	}
	var photos embed.Model
	if cfg.AI.EmbedOn() {
		photos = aisvc.Photos{AI: aiClient, Dims: catalogdomain.PhotoDims, Timeout: cfg.Embed.Timeout}
	}
	catalogm := catalog.Mount(d, catalog.Settings{
		Text:        embedder(cfg.Embed.IndexAPIKey),
		Image:       photos,
		TextVersion: cfg.Embed.TextVersion, ImageVersion: cfg.Embed.ImageVersion, EmbedSlots: cfg.Embed.Slots,
		IndexNow: catalogsvc.IndexNowSettings{
			Key: cfg.IndexNowKey, Site: website, Client: indexnow.New(httpx.NewClient(30*time.Second), indexnow.Endpoint),
			Every: catalogsvc.IndexNowEvery,
		},
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
	// Module deal mounts after chat and uses it; chat reads linked orders through deals once it is set.
	chatm := chat.Mount(d, chat.Settings{WebsiteOrigins: cfg.ObjectStore.UploadOrigins, Orders: func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]chatsvc.OrderRef, error) {
		return orderRefs(ctx, deals, ids)
	}}, accounts, catalogm.Service, mediam.Service)
	dealm := deal.Mount(d, deal.Settings{Shipping: shippingm.Service, FakeCarrier: cfg.Carrier.Mode == "fake", PollEvery: cfg.Carrier.PollEvery},
		accounts, catalogm.Service, chatm.Service)
	deals = dealm.Service
	erasure.Mount(d, accounts, dealm.Service)
	beacon.Mount(d)
	supportm := support.Mount(d, accounts, chatm.Service, dealm.Service, catalogm.Service)
	trustm := trust.Mount(d, trust.Settings{
		Classifier: trust.LLMClassifier(
			llm.OpenAI{BaseURL: cfg.Trust.LLMBaseURL, APIKey: cfg.Trust.LLMAPIKey, Model: cfg.Trust.LLMModel, HTTP: httpx.NewClient(0)},
			cfg.Trust.LLMModel, cfg.Trust.CallTimeout, log),
		TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity,
	}, accounts, catalogm.Service, dealm.Service, chatm.Service, mediam.Service)
	consolem := console.Mount(d, console.Settings{Keys: consoleKeys, Config: cfg.Console}, accounts, supportm.Service, trustm.Service,
		catalogm.Service)
	model := llm.OpenAI{BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey, Model: cfg.LLM.Model, Effort: cfg.LLM.Effort, HTTP: httpx.NewClient(0)}
	discoverym := discovery.Mount(d, discovery.Settings{
		Model:       model,
		Text:        embedder(cfg.Embed.SearchAPIKey),
		Image:       photos,
		TextVersion: cfg.Embed.TextVersion, ImageVersion: cfg.Embed.ImageVersion,
		TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity, LookMin: cfg.Embed.LookMinSimilarity,
	}, catalogm.Service, mediam.Service, accounts)
	assistm := assist.Mount(d, assist.Settings{
		AI: aiClient,
		Model: llm.OpenAI{
			BaseURL: cfg.Assist.LLMBaseURL, APIKey: cfg.Assist.LLMAPIKey, Model: cfg.Assist.LLMModel,
			Effort: cfg.Assist.LLMEffort, HTTP: httpx.NewClient(0),
		},
		Relay:   rel,
		Text:    embedder(cfg.Embed.SearchAPIKey),
		Image:   photos,
		TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity, CallTimeout: cfg.Assist.CallTimeout,
		Source: chotot.Client{
			GatewayURL: cfg.Assist.ChototGatewayURL, CDNURL: cfg.Assist.ChototCDNURL, HTTP: httpx.NewClient(0),
		},
	}, accounts, mediam.Service, catalogm.Service, discoverym.Service)
	updates := stream.PGLog{DB: pool}
	router.Handle("GET /v1/ws", stream.SubprotocolToken(d.Protect(stream.Socket{
		Hub: hub, Updates: updates, Access: socketAccess{accounts: accounts, chats: chatm.Service},
		Verify: account.TokenVerifier(cfg.Auth), Tick: time.Minute, Origins: cfg.ObjectStore.UploadOrigins,
		Now: time.Now, Log: log,
	})))
	router.Handle("GET /v1/updates", d.Protect(stream.UpdatesHandler(updates, log)))
	if cfg.Env != config.EnvProd {
		router.Handle("POST /v1/dev/updates", d.Protect(stream.DevUpdateHandler(updates, log)))
	}
	notificationm := notification.Mount(d, notification.Settings{Push: sender, Email: mailer, Site: website},
		accounts, catalogm.Service, dealm.Service, trustm.Service, chatm.Service, discoverym.Service, supportm.Service)
	screen.Mount(d, accounts, catalogm.Service, dealm.Service, trustm.Service, discoverym.Service, chatm.Service, notificationm.Service,
		supportm.Service, shippingm.Service)
	agentm, agentErr := agent.Mount(d, agent.Settings{
		Config: cfg.Agent, TextMin: cfg.Embed.TextMinSimilarity, PhotoMin: cfg.Embed.PhotoMinSimilarity,
	}, consolem.Moderator, consolem.Admin, consolem.Service, supportm.Service, accounts, catalogm.Service, dealm.Service, shippingm.Service, trustm.Service)
	if agentErr != nil {
		return nil, nil, fmt.Errorf("agent: %w", agentErr)
	}
	return slices.Concat(accountm.Services, mediam.Services, catalogm.Services, shippingm.Services, dealm.Services, discoverym.Services,
		chatm.Services, supportm.Services, trustm.Services, notificationm.Services, assistm.Services, aiServices), agentm, nil
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
	if a.pool != nil {
		if err := a.pool.Ping(ctx); err != nil {
			return fmt.Errorf("ping postgres: %w", err)
		}
	}
	if err := restatex.Healthy(ctx, a.adminURL); err != nil {
		return fmt.Errorf("check restate: %w", err)
	}
	return nil
}

// CloseStreams ends every open WebSocket with 1001, and every read's event
// stream, so a server shutdown does not wait for them; the server's RegisterOnShutdown calls it.
func (a *App) CloseStreams() {
	if a.hub != nil {
		a.hub.Close()
	}
	if a.relay != nil {
		a.relay.Close()
	}
}

// Close ends the streams, stops the event bus, the stream hub, the agent's
// MCP server and the Restate endpoint, then releases the models and closes
// the pool and the country database.
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
	if a.closeAI != nil {
		a.closeAI()
	}
	if a.pool != nil {
		a.pool.Close()
	}
	if cerr := a.geo.Close(); cerr != nil {
		err = errors.Join(err, cerr)
	}
	return err
}

// orderRefs reads orders for chat's link cards.
func orderRefs(ctx context.Context, deals dealsvc.Service, ids []uuid.UUID) (map[uuid.UUID]chatsvc.OrderRef, error) {
	out := make(map[uuid.UUID]chatsvc.OrderRef, len(ids))
	for _, id := range ids {
		o, ok, err := deals.Order(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("read order %s: %w", id, err)
		}
		if ok {
			out[id] = chatsvc.OrderRef{BuyerID: o.BuyerID, SellerID: o.SellerID, ListingID: o.ListingID, Code: o.Code, Status: string(o.Status), Price: o.Price}
		}
	}
	return out, nil
}
