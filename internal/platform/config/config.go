// Package config reads the process configuration from environment variables,
// once, at start.
package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Env is APP_ENV.
type Env string

const (
	EnvDev  Env = "dev"
	EnvTest Env = "test"
	EnvProd Env = "prod"
)

// LogFormat is LOG_FORMAT.
type LogFormat string

const (
	LogText LogFormat = "text"
	LogJSON LogFormat = "json"
)

// Role is SERVER_ROLE: what this process runs. Unset or empty is all.
type Role string

const (
	// RoleAll runs every module, the HTTP API, every Restate service and
	// module ai with its models: compose.
	RoleAll Role = "all"
	// RoleAPI runs everything but module ai: no models, no Restate service
	// ai. The cluster's Deployment server.
	RoleAPI Role = "api"
	// RoleAI runs module ai alone: its models and the Restate service ai,
	// and on HTTP_ADDR only /healthz and /readyz. The cluster's server-ai.
	RoleAI Role = "ai"
)

// ServesAPI says whether the process runs the modules other than ai.
func (r Role) ServesAPI() bool { return r != RoleAI }

// ServesAI says whether the process loads the models and serves the
// Restate service ai.
func (r Role) ServesAI() bool { return r != RoleAPI }

// Config holds every setting the server reads. Modules receive the parts they
// need through their constructors and never read the environment themselves.
type Config struct {
	Env         Env
	Role        Role       //exhaustruct:optional SERVER_ROLE; the zero value is all
	HTTPAddr    string     // HTTP_ADDR
	LogLevel    slog.Level // LOG_LEVEL: debug | info | warn | error
	LogFormat   LogFormat
	DatabaseURL string // DATABASE_URL
	// DatabaseMaxConns is DATABASE_MAX_CONNS, the most connections this
	// process's pool opens. Every replica opens its own pool.
	DatabaseMaxConns int
	OTLPEndpoint     string // OTEL_EXPORTER_OTLP_ENDPOINT; empty disables export
	ServiceName      string // SERVICE_NAME
	Restate          RestateConfig
	TrustedProxies   []netip.Prefix // TRUSTED_PROXIES: CIDRs whose X-Forwarded-For is believed
	// GeoIPDB is GEOIP_DB, a country database in MaxMind's format that the
	// access log reads each visitor's country from. Empty logs no country.
	GeoIPDB string //exhaustruct:optional
	// DocsOrigins is DOCS_ORIGINS, the origins of the docs site, comma
	// separated. Its API playground calls /v1/ from these origins, without
	// cookies. Empty allows none.
	DocsOrigins []string //exhaustruct:optional
	Auth        AuthConfig
	Console     ConsoleConfig
	ObjectStore ObjectStoreConfig
	LLM         LLMConfig
	Embed       EmbedConfig
	Assist      AssistConfig
	AI          AIConfig //exhaustruct:optional
	Trust       TrustConfig
	Agent       AgentConfig
	Push        PushConfig
	Firebase    FirebaseConfig
	Email       EmailConfig
	Google      GoogleConfig
	Carrier     CarrierConfig
	// IndexNowKey is INDEXNOW_KEY, the key catalog tells search engines
	// changed listing pages with (8 to 128 letters, digits and dashes).
	// Empty turns IndexNow off.
	IndexNowKey string //exhaustruct:optional
}

// CarrierConfig says where shipments are made and read, on the accounts
// sellers link at GHTK and GHN. The tokens and the key are never logged.
type CarrierConfig struct {
	// GHTKMode and GHNMode are CARRIER_GHTK and CARRIER_GHN, each live,
	// fake or off. fake (dev and test) links any account but the password
	// or token "wrong" and knows every tracking code but those that start
	// with NOTFOUND, and with it the server serves the dev endpoint that
	// moves its shipments; APP_ENV=prod refuses it. off makes and reads no
	// shipment at that carrier. live calls the carrier with its settings.
	GHTKMode string
	GHNMode  string
	// TokenKey is CARRIER_TOKEN_KEY: base64 of 32 bytes, the AES-GCM key
	// of the carrier tokens sellers link; one per environment.
	TokenKey []byte
	// PollEvery is CARRIER_POLL_EVERY: how often a shipped order's
	// shipment is read from its carrier, under the carriers' reports.
	PollEvery time.Duration
	// With CARRIER_GHTK=live only, empty otherwise.
	GHTKURL     string //exhaustruct:optional GHTK_API_URL
	GHTKPartner string //exhaustruct:optional GHTK_PARTNER_TOKEN: ShopNexus's B2C token
	GHTKSource  string //exhaustruct:optional GHTK_CLIENT_SOURCE: ShopNexus's partner code
	GHTKWebhook string //exhaustruct:optional GHTK_WEBHOOK_SECRET: the secret in GHTK's webhook path
	// With CARRIER_GHN=live only, empty otherwise.
	GHNURL     string //exhaustruct:optional GHN_API_URL: ends in /shiip/public-api
	GHNPartner string //exhaustruct:optional GHN_PARTNER_TOKEN: ShopNexus's own GHN token
	GHNWebhook string //exhaustruct:optional GHN_WEBHOOK_SECRET: the secret in GHN's webhook path
}

// PushConfig says how pushes reach phones. Empty credentials mean the log
// sender. The credentials are never logged.
type PushConfig struct {
	Credentials []byte //exhaustruct:optional FCM_CREDENTIALS: a service account's JSON in base64, or empty
}

// FirebaseConfig names the Firebase project that pushes go through and phone
// tokens come from, and where the keys that sign those tokens are published.
type FirebaseConfig struct {
	ProjectID     string // FIREBASE_PROJECT_ID
	ProjectNumber string // FIREBASE_PROJECT_NUMBER: builds the issuer of PNV tokens
	AuthJWKSURL   string // FIREBASE_AUTH_JWKS_URL: the keys of Firebase Authentication's ID tokens
	PNVJWKSURL    string // PNV_JWKS_URL: the keys of Phone Number Verification tokens
	TestNumbers   bool   // PNV_TEST_NUMBERS: accept the test numbers of PNV's test mode; never in prod
}

// EmailConfig picks how the server mails sign-in codes and notices.
type EmailConfig struct {
	Sender       string // EMAIL_SENDER: log or resend; APP_ENV=prod refuses log
	ResendAPIKey string // RESEND_API_KEY: required with EMAIL_SENDER=resend, may be empty otherwise
	From         string // EMAIL_FROM: the sender, an address of a domain verified at Resend
}

// GoogleConfig names the OAuth client whose Google ID tokens sign in.
type GoogleConfig struct {
	ClientID string // GOOGLE_CLIENT_ID: the Web client ID the app passes to Credential Manager; empty turns Google sign-in off
	JWKSURL  string // GOOGLE_JWKS_URL: the keys of Google's ID tokens
}

// LLMConfig says where the model that reads search queries is. The key is
// never logged.
type LLMConfig struct {
	BaseURL string // LLM_BASE_URL: an OpenAI-compatible API, such as https://proxy/v1
	APIKey  string // LLM_API_KEY
	Model   string // LLM_MODEL
	// LLM_REASONING_EFFORT: none, low, medium or high; empty is the model's default
	Effort string
}

// EmbedConfig says where the text embedding model is and how search uses
// both kinds of vector. Text goes to LiteLLM's alias embed-text; photos go
// to module ai (AIConfig). Each kind can be off: an empty TextModel, or an
// empty AI_EMBED_MODELS, turns it off, and its base URL, version and floor
// may then be empty too. An off kind writes and reads no vectors.
type EmbedConfig struct {
	TextBaseURL string // EMBED_TEXT_BASE_URL: POST {base}/embeddings for text
	TextModel   string // EMBED_TEXT_MODEL, the alias embed-text; empty turns text vectors off
	// TextVersion and ImageVersion name the model behind each kind. Every
	// vector row stores its version; a new version embeds again.
	TextVersion  string        // EMBED_TEXT_VERSION
	ImageVersion string        // EMBED_IMAGE_VERSION
	IndexAPIKey  string        // EMBED_INDEX_API_KEY: ListingEmbed's key; empty locally
	SearchAPIKey string        // EMBED_SEARCH_API_KEY: the search's key; empty locally
	Timeout      time.Duration // EMBED_TIMEOUT: one embedding call
	Slots        int           // EMBED_SLOTS: embeddings ListingEmbed runs at once in this process
	// TextMinSimilarity and PhotoMinSimilarity are the cosine a listing needs
	// to join a vector list. LookMinSimilarity is the photo list's floor when
	// the query is the look the search model wrote, a text.
	TextMinSimilarity  float64 // EMBED_TEXT_MIN_SIMILARITY, in (0, 1)
	PhotoMinSimilarity float64 // EMBED_PHOTO_MIN_SIMILARITY, in (0, 1)
	LookMinSimilarity  float64 // EMBED_LOOK_MIN_SIMILARITY, in (0, 1)
}

// TextOn says whether text vectors are on; AIConfig.EmbedOn says it of
// photo vectors.
func (c EmbedConfig) TextOn() bool { return c.TextModel != "" }

// AIConfig is module ai's models and limits. Every role reads it; only a
// process with SERVER_ROLE all or ai loads the models. A model is name=dir,
// its folder under /models in the image's stacks.
type AIConfig struct {
	// STTModel and STTDir are AI_STT_MODELS, one name=dir: Gipformer's
	// encoder, decoder, joiner and tokens.
	STTModel, STTDir string
	VADModel         string // AI_VAD_MODEL: path of silero_vad.onnx
	NumThreads       int    // AI_NUM_THREADS: onnxruntime threads per run
	MaxConcurrent    int    // AI_MAX_CONCURRENT: transcriptions decoding at once
	MaxAudioSeconds  int    // AI_MAX_AUDIO_SECONDS: longest audio heard
	// EmbedModel and EmbedDir are AI_EMBED_MODELS, one name=dir of SigLIP 2,
	// or empty: photo vectors are off and SigLIP 2 is not loaded.
	EmbedModel, EmbedDir string  //exhaustruct:optional
	EmbedMaxInputs       int     // AI_EMBED_MAX_INPUTS: images and texts per EmbedImages call
	EmbedMaxConcurrent   int     // AI_EMBED_MAX_CONCURRENT: calls embedding at once
	EmbedMaxImageMB      int     // AI_EMBED_MAX_IMAGE_MB: one image after base64 decoding
	FramesMaxConcurrent  int     // AI_FRAMES_MAX_CONCURRENT: videos cut into frames at once
	FramesMaxSeconds     int     // AI_FRAMES_MAX_SECONDS: longest video
	FramesMinSharpness   float64 // AI_FRAMES_MIN_SHARPNESS: Laplacian variance under which a frame is blurred
}

// EmbedOn says whether photo vectors are on.
func (c AIConfig) EmbedOn() bool { return c.EmbedModel != "" }

// AssistConfig says where module assist's model is, the one that fills a
// listing from frames and speech or from the seller's listing on Chợ Tốt,
// and where Chợ Tốt is. Frames and speech are module ai's. The key is
// never logged.
type AssistConfig struct {
	LLMBaseURL string // ASSIST_LLM_BASE_URL: an OpenAI-compatible API whose model reads images
	LLMAPIKey  string // ASSIST_LLM_API_KEY: server-assist's key
	LLMModel   string // ASSIST_LLM_MODEL: the alias video-read
	// ASSIST_LLM_REASONING_EFFORT: none, low, medium or high; empty is the model's default
	LLMEffort   string
	CallTimeout time.Duration // ASSIST_CALL_TIMEOUT: one call to ai, the model or Chợ Tốt
	// CHOTOT_GATEWAY_URL: Chợ Tốt's public gateway, https://gateway.chotot.com
	ChototGatewayURL string
	// CHOTOT_CDN_URL: where Chợ Tốt's photos are, https://cdn.chotot.com; no other origin is read
	ChototCDNURL string
}

// TrustConfig says where module trust's scam classifier is: an
// OpenAI-compatible API, LiteLLM's alias scam-check in the cluster. The key
// is never logged.
type TrustConfig struct {
	LLMBaseURL  string        // TRUST_LLM_BASE_URL
	LLMAPIKey   string        // TRUST_LLM_API_KEY: server-trust's key
	LLMModel    string        // TRUST_LLM_MODEL: the alias scam-check
	CallTimeout time.Duration // TRUST_CALL_TIMEOUT: one call to the model
}

// AgentConfig says where the console agent is and how it and the server
// trust each other, and where LiteLLM's admin API is for the AI settings
// (see ARCHITECTURE.md, Console agent and AI settings). No token or key is
// ever logged.
type AgentConfig struct {
	// BaseURL is AGENT_BASE_URL, the agent service on the internal
	// network; the console's agent endpoints forward there.
	BaseURL string
	// ServiceToken is AGENT_SERVICE_TOKEN, the bearer token the server
	// sends the agent; at least 32 characters.
	ServiceToken string
	// MCPAddr is AGENT_MCP_ADDR, the address the MCP server listens on,
	// apart from HTTP_ADDR and never behind the ingress.
	MCPAddr string
	// MCPToken is AGENT_MCP_TOKEN, the bearer token the agent sends the
	// MCP server; at least 32 characters.
	MCPToken string
	// LiteLLMURL is LITELLM_URL, LiteLLM's base URL for its admin API (no
	// /v1), which the AI settings drive.
	LiteLLMURL string
	// LiteLLMMasterKey is LITELLM_MASTER_KEY, LiteLLM's master key.
	LiteLLMMasterKey string
}

// AuthConfig holds the auth module's keys and token settings. No key is ever
// logged.
type AuthConfig struct {
	OTPKey       []byte             // OTP_KEY: base64, at least 32 bytes; hashes and derives email codes
	JWTKeyID     string             // JWT_KEY_ID: the kid of JWTKey
	JWTKey       ed25519.PrivateKey // JWT_PRIVATE_KEY: base64 of the 32-byte seed
	JWTPrevKeyID string             //exhaustruct:optional JWT_PREVIOUS_KEY_ID: empty outside a key change
	JWTPrevKey   ed25519.PublicKey  //exhaustruct:optional JWT_PREVIOUS_PUBLIC_KEY: base64, empty with the ID
	Issuer       string             // JWT_ISSUER
	Audience     string             // JWT_AUDIENCE
}

// ConsoleConfig holds module console's key, the console's origin and the
// hosts of the ops dashboards. The key is never logged.
type ConsoleConfig struct {
	Key    []byte // CONSOLE_KEY: base64, at least 32 bytes; the console subkeys derive from it
	Origin string // CONSOLE_ORIGIN: the web console's origin, the only one CORS allows
	// DashboardHosts is DASHBOARD_HOSTS, "grafana=grafana.x.vn,restate=...":
	// the host each ops dashboard is served at, keyed by one of
	// DashboardApps, host names lower-cased. Empty refuses every dashboard.
	DashboardHosts map[string]string //exhaustruct:optional
	// SkipTOTP is CONSOLE_SKIP_TOTP=true: a member signs in with the email
	// and password alone, with no TOTP step. Unset or false keeps TOTP.
	SkipTOTP bool //exhaustruct:optional
}

// DashboardApps are the ops dashboards DASHBOARD_HOSTS may name. Module
// console gives each the role that opens it.
var DashboardApps = []string{"grafana", "restate", "litellm", "s3", "argocd", "db"}

// ObjectStoreConfig says where the S3-compatible object store (RustFS) is
// and where clients read public objects. The keys are never logged.
type ObjectStoreConfig struct {
	Endpoint string // OBJECT_STORE_ENDPOINT: its S3 API, http:// or https://
	// PublicEndpoint is OBJECT_STORE_PUBLIC_ENDPOINT, the same API as clients
	// reach it; presigned URLs sign its host.
	PublicEndpoint string
	Region         string // OBJECT_STORE_REGION
	AccessKey      string // OBJECT_STORE_ACCESS_KEY
	SecretKey      string // OBJECT_STORE_SECRET_KEY
	Bucket         string // OBJECT_STORE_BUCKET
	PublicURL      string // MEDIA_PUBLIC_URL: the base URL clients read media/ objects under
	// UploadOrigins is WEBSITE_ORIGINS, the website's origins, comma
	// separated. Browsers there PUT uploads to presigned URLs, so the
	// bucket's CORS allows them, and open GET /v1/ws. Empty allows none.
	UploadOrigins []string //exhaustruct:optional
}

// RestateConfig says where the Restate server is and how it reaches this
// process.
type RestateConfig struct {
	IngressURL string // RESTATE_INGRESS_URL
	AdminURL   string // RESTATE_ADMIN_URL
	ListenAddr string // RESTATE_LISTEN_ADDR: where this process serves its Restate endpoint
	// EndpointURL (RESTATE_ENDPOINT_URL) is how Restate reaches the endpoint,
	// the URL registered at start. Empty registers http:// and the address
	// the endpoint is bound to, a ListenAddr port of 0 resolved.
	EndpointURL string
	// ServicePrefix (RESTATE_SERVICE_PREFIX) goes before every service name,
	// so servers sharing one Restate server, as tests do, keep apart.
	ServicePrefix string
	// IdentityKey (RESTATE_IDENTITY_KEY, "publickeyv1_...") is the public key
	// the Restate server signs requests with; the endpoint refuses unsigned
	// ones. It may be empty outside APP_ENV=prod; make run writes it to .env.
	IdentityKey string
	// Register (RESTATE_REGISTER) is true when this process registers its
	// endpoint with the Restate admin API at start, as make run does, and
	// false when something else registers it, as restate-operator does in
	// the cluster.
	Register bool
}

// Load builds a Config from lookup, which is [os.LookupEnv] outside tests.
// Every variable must be set; none has a default. A variable documented as
// allowing empty may be set to the empty string. Load reports every missing
// or invalid variable in one error.
// indexNowKey is the shape IndexNow takes for a key.
var indexNowKey = regexp.MustCompile(`^[a-zA-Z0-9-]{8,128}$`)

func Load(lookup func(string) (string, bool)) (Config, error) {
	var errs []error
	// set returns a variable that may be empty.
	set := func(key string) string {
		v, ok := lookup(key)
		if !ok {
			errs = append(errs, fmt.Errorf("%s is not set", key))
		}
		return v
	}
	need := func(key string) string {
		v, ok := lookup(key)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s is not set", key))
		case v == "":
			errs = append(errs, fmt.Errorf("%s is empty", key))
		default:
		}
		return v
	}
	oneOf := func(key string, allowed ...string) string {
		v, ok := lookup(key)
		if !ok {
			errs = append(errs, fmt.Errorf("%s is not set", key))
		} else if !slices.Contains(allowed, v) {
			errs = append(errs, fmt.Errorf("%s=%q: want one of %v", key, v, allowed))
		}
		return v
	}
	optBool := func(key string) bool {
		v, _ := lookup(key)
		if !slices.Contains([]string{"", "true", "false"}, v) {
			errs = append(errs, fmt.Errorf("%s=%q: want true, false or empty", key, v))
		}
		return v == "true"
	}
	positive := func(key string) int {
		v := need(key)
		n, err := strconv.Atoi(v)
		if v != "" && (err != nil || n < 1) {
			errs = append(errs, fmt.Errorf("%s=%q: want a positive integer", key, v))
		}
		return n
	}
	duration := func(key string) time.Duration {
		v := need(key)
		d, err := time.ParseDuration(v)
		if v != "" && (err != nil || d <= 0) {
			errs = append(errs, fmt.Errorf("%s=%q: want a positive duration such as 10s", key, v))
		}
		return d
	}
	// token is a shared secret of at least 32 characters.
	token := func(key string) string {
		v := need(key)
		if v != "" && len(v) < 32 {
			errs = append(errs, fmt.Errorf("%s: want at least 32 characters; make run adds it to .env", key))
		}
		return v
	}
	fraction := func(key string) float64 {
		v := need(key)
		f, err := strconv.ParseFloat(v, 64)
		if v != "" && (err != nil || f <= 0 || f >= 1) {
			errs = append(errs, fmt.Errorf("%s=%q: want a number between 0 and 1", key, v))
		}
		return f
	}

	nonNegative := func(key string) float64 {
		v := need(key)
		f, err := strconv.ParseFloat(v, 64)
		if v != "" && (err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0)) {
			errs = append(errs, fmt.Errorf("%s=%q: want a number of zero or more", key, v))
		}
		return f
	}
	// model reads one name=dir; empty is none when it may be.
	model := func(key string, mayBeEmpty bool) (name, dir string) {
		var v string
		if mayBeEmpty {
			v = set(key)
		} else {
			v = need(key)
		}
		if v = strings.TrimSpace(v); v == "" {
			return "", ""
		}
		name, dir, ok := strings.Cut(v, "=")
		name, dir = strings.TrimSpace(name), strings.TrimSpace(dir)
		if !ok || name == "" || dir == "" || strings.Contains(dir, ",") {
			errs = append(errs, fmt.Errorf("%s=%q: want one model as name=dir", key, v))
		}
		return name, dir
	}

	role := RoleAll
	if v, _ := lookup("SERVER_ROLE"); v != "" {
		role = Role(v)
		if !slices.Contains([]Role{RoleAll, RoleAPI, RoleAI}, role) {
			errs = append(errs, fmt.Errorf("SERVER_ROLE=%q: want all, api or ai", v))
		}
	}

	var ai AIConfig
	ai.STTModel, ai.STTDir = model("AI_STT_MODELS", false)
	ai.EmbedModel, ai.EmbedDir = model("AI_EMBED_MODELS", true)
	ai.VADModel = need("AI_VAD_MODEL")
	ai.NumThreads, ai.MaxConcurrent = positive("AI_NUM_THREADS"), positive("AI_MAX_CONCURRENT")
	ai.MaxAudioSeconds = positive("AI_MAX_AUDIO_SECONDS")
	ai.EmbedMaxInputs, ai.EmbedMaxConcurrent = positive("AI_EMBED_MAX_INPUTS"), positive("AI_EMBED_MAX_CONCURRENT")
	ai.EmbedMaxImageMB = positive("AI_EMBED_MAX_IMAGE_MB")
	ai.FramesMaxConcurrent, ai.FramesMaxSeconds = positive("AI_FRAMES_MAX_CONCURRENT"), positive("AI_FRAMES_MAX_SECONDS")
	ai.FramesMinSharpness = nonNegative("AI_FRAMES_MIN_SHARPNESS")

	// An embedding kind is on when its model is set; the rest of the kind's
	// settings are then needed, and may be empty when it is off.
	textModel, imageModel := set("EMBED_TEXT_MODEL"), ai.EmbedModel
	forKind := func(model, key string) string {
		if model == "" {
			return set(key)
		}
		return need(key)
	}
	fractionForKind := func(model, key string) float64 {
		if model == "" && set(key) == "" {
			return 0
		}
		return fraction(key)
	}

	decodeKey := func(key string, size int, exact bool) []byte {
		v, ok := lookup(key)
		if !ok || v == "" {
			errs = append(errs, fmt.Errorf("%s is not set; make run adds it to .env", key))
			return nil
		}
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(b) < size || (exact && len(b) != size) {
			want := fmt.Sprintf("at least %d", size)
			if exact {
				want = strconv.Itoa(size)
			}
			errs = append(errs, fmt.Errorf("%s: want base64 of %s bytes", key, want))
			return nil
		}
		return b
	}

	var logLevel slog.Level
	// oneOf has checked the value, so it parses.
	_ = logLevel.UnmarshalText([]byte(oneOf("LOG_LEVEL", "debug", "info", "warn", "error")))

	var proxies []netip.Prefix
	for s := range strings.SplitSeq(set("TRUSTED_PROXIES"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %q is not a CIDR", s))
			continue
		}
		proxies = append(proxies, p.Masked())
	}

	origins := func(key string) []string {
		var out []string
		for s := range strings.SplitSeq(set(key), ",") {
			if s = strings.TrimSpace(s); s == "" {
				continue
			}
			u, err := url.Parse(s)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" ||
				u.RawQuery != "" || u.User != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not an origin like https://example.com", key, s))
				continue
			}
			out = append(out, s)
		}
		return out
	}
	docsOrigins := origins("DOCS_ORIGINS")
	uploadOrigins := origins("WEBSITE_ORIGINS")

	var dashboardHosts map[string]string
	for pair := range strings.SplitSeq(set("DASHBOARD_HOSTS"), ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		app, host, _ := strings.Cut(pair, "=")
		app, host = strings.TrimSpace(app), strings.ToLower(strings.TrimSpace(host))
		switch {
		case !slices.Contains(DashboardApps, app):
			errs = append(errs, fmt.Errorf("DASHBOARD_HOSTS: %q: want app=host, the app one of %v", pair, DashboardApps))
		case !isHostName(host):
			errs = append(errs, fmt.Errorf("DASHBOARD_HOSTS: %q: %q is not a host name", pair, host))
		case dashboardHosts[app] != "":
			errs = append(errs, fmt.Errorf("DASHBOARD_HOSTS names %s twice", app))
		case slices.Contains(slices.Collect(maps.Values(dashboardHosts)), host):
			errs = append(errs, fmt.Errorf("DASHBOARD_HOSTS gives %s to two dashboards", host))
		default:
			if dashboardHosts == nil {
				dashboardHosts = map[string]string{}
			}
			dashboardHosts[app] = host
		}
	}

	var jwtKey ed25519.PrivateKey
	if seed := decodeKey("JWT_PRIVATE_KEY", ed25519.SeedSize, true); seed != nil {
		jwtKey = ed25519.NewKeyFromSeed(seed)
	}

	var push PushConfig
	if raw := set("FCM_CREDENTIALS"); raw != "" {
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			errs = append(errs, errors.New("FCM_CREDENTIALS: want base64 of a service account's JSON"))
		}
		push.Credentials = b
	}

	cfg := Config{
		Env:              Env(oneOf("APP_ENV", string(EnvDev), string(EnvTest), string(EnvProd))),
		Role:             role,
		AI:               ai,
		HTTPAddr:         need("HTTP_ADDR"),
		LogLevel:         logLevel,
		LogFormat:        LogFormat(oneOf("LOG_FORMAT", string(LogText), string(LogJSON))),
		DatabaseURL:      need("DATABASE_URL"),
		DatabaseMaxConns: positive("DATABASE_MAX_CONNS"),
		Restate: RestateConfig{
			IngressURL:    need("RESTATE_INGRESS_URL"),
			AdminURL:      need("RESTATE_ADMIN_URL"),
			ListenAddr:    need("RESTATE_LISTEN_ADDR"),
			EndpointURL:   set("RESTATE_ENDPOINT_URL"),
			ServicePrefix: set("RESTATE_SERVICE_PREFIX"),
			IdentityKey:   set("RESTATE_IDENTITY_KEY"),
			Register:      oneOf("RESTATE_REGISTER", "true", "false") == "true",
		},
		Auth: AuthConfig{
			OTPKey:       decodeKey("OTP_KEY", 32, false),
			JWTKeyID:     need("JWT_KEY_ID"),
			JWTKey:       jwtKey,
			JWTPrevKeyID: set("JWT_PREVIOUS_KEY_ID"),
			Issuer:       need("JWT_ISSUER"),
			Audience:     need("JWT_AUDIENCE"),
		},
		Console: ConsoleConfig{
			Key:            decodeKey("CONSOLE_KEY", 32, false),
			Origin:         need("CONSOLE_ORIGIN"),
			DashboardHosts: dashboardHosts,
			SkipTOTP:       optBool("CONSOLE_SKIP_TOTP"),
		},
		ObjectStore: ObjectStoreConfig{
			Endpoint:       need("OBJECT_STORE_ENDPOINT"),
			PublicEndpoint: need("OBJECT_STORE_PUBLIC_ENDPOINT"),
			Region:         need("OBJECT_STORE_REGION"),
			AccessKey:      need("OBJECT_STORE_ACCESS_KEY"),
			SecretKey:      need("OBJECT_STORE_SECRET_KEY"),
			Bucket:         need("OBJECT_STORE_BUCKET"),
			PublicURL:      need("MEDIA_PUBLIC_URL"),
			UploadOrigins:  uploadOrigins,
		},
		Push: push,
		Firebase: FirebaseConfig{
			ProjectID:     need("FIREBASE_PROJECT_ID"),
			ProjectNumber: need("FIREBASE_PROJECT_NUMBER"),
			AuthJWKSURL:   need("FIREBASE_AUTH_JWKS_URL"),
			PNVJWKSURL:    need("PNV_JWKS_URL"),
			TestNumbers:   oneOf("PNV_TEST_NUMBERS", "true", "false") == "true",
		},
		Email: EmailConfig{
			Sender:       oneOf("EMAIL_SENDER", "log", "resend"),
			ResendAPIKey: set("RESEND_API_KEY"),
			From:         need("EMAIL_FROM"),
		},
		Google: GoogleConfig{ClientID: set("GOOGLE_CLIENT_ID"), JWKSURL: need("GOOGLE_JWKS_URL")},
		Carrier: CarrierConfig{
			GHTKMode: oneOf("CARRIER_GHTK", "live", "fake", "off"), GHNMode: oneOf("CARRIER_GHN", "live", "fake", "off"),
			TokenKey: decodeKey("CARRIER_TOKEN_KEY", 32, true), PollEvery: duration("CARRIER_POLL_EVERY"),
			GHTKURL: set("GHTK_API_URL"), GHTKPartner: set("GHTK_PARTNER_TOKEN"), GHTKSource: set("GHTK_CLIENT_SOURCE"),
			GHTKWebhook: set("GHTK_WEBHOOK_SECRET"), GHNURL: set("GHN_API_URL"), GHNPartner: set("GHN_PARTNER_TOKEN"), GHNWebhook: set("GHN_WEBHOOK_SECRET"),
		},
		LLM: LLMConfig{
			BaseURL: need("LLM_BASE_URL"), APIKey: need("LLM_API_KEY"), Model: need("LLM_MODEL"),
			Effort: oneOf("LLM_REASONING_EFFORT", "", "none", "low", "medium", "high"),
		},
		Embed: EmbedConfig{
			TextBaseURL: forKind(textModel, "EMBED_TEXT_BASE_URL"), TextModel: textModel,
			TextVersion: forKind(textModel, "EMBED_TEXT_VERSION"), ImageVersion: forKind(imageModel, "EMBED_IMAGE_VERSION"),
			IndexAPIKey: set("EMBED_INDEX_API_KEY"), SearchAPIKey: set("EMBED_SEARCH_API_KEY"),
			Timeout: duration("EMBED_TIMEOUT"), Slots: positive("EMBED_SLOTS"),
			TextMinSimilarity:  fractionForKind(textModel, "EMBED_TEXT_MIN_SIMILARITY"),
			PhotoMinSimilarity: fractionForKind(imageModel, "EMBED_PHOTO_MIN_SIMILARITY"),
			LookMinSimilarity:  fractionForKind(imageModel, "EMBED_LOOK_MIN_SIMILARITY"),
		},
		Assist: AssistConfig{
			LLMBaseURL: need("ASSIST_LLM_BASE_URL"), LLMAPIKey: need("ASSIST_LLM_API_KEY"), LLMModel: need("ASSIST_LLM_MODEL"),
			LLMEffort:        oneOf("ASSIST_LLM_REASONING_EFFORT", "", "none", "low", "medium", "high"),
			CallTimeout:      duration("ASSIST_CALL_TIMEOUT"),
			ChototGatewayURL: need("CHOTOT_GATEWAY_URL"), ChototCDNURL: need("CHOTOT_CDN_URL"),
		},
		Trust: TrustConfig{
			LLMBaseURL: need("TRUST_LLM_BASE_URL"), LLMAPIKey: need("TRUST_LLM_API_KEY"), LLMModel: need("TRUST_LLM_MODEL"),
			CallTimeout: duration("TRUST_CALL_TIMEOUT"),
		},
		Agent: AgentConfig{
			BaseURL: need("AGENT_BASE_URL"), ServiceToken: token("AGENT_SERVICE_TOKEN"),
			MCPAddr: need("AGENT_MCP_ADDR"), MCPToken: token("AGENT_MCP_TOKEN"),
			LiteLLMURL: need("LITELLM_URL"), LiteLLMMasterKey: need("LITELLM_MASTER_KEY"),
		},
		OTLPEndpoint:   set("OTEL_EXPORTER_OTLP_ENDPOINT"),
		ServiceName:    need("SERVICE_NAME"),
		TrustedProxies: proxies,
		DocsOrigins:    docsOrigins,
		IndexNowKey:    set("INDEXNOW_KEY"),
		GeoIPDB:        set("GEOIP_DB"),
	}
	if k := cfg.IndexNowKey; k != "" && !indexNowKey.MatchString(k) {
		errs = append(errs, errors.New("INDEXNOW_KEY: want 8 to 128 letters, digits and dashes, or empty"))
	}

	prevKey := set("JWT_PREVIOUS_PUBLIC_KEY")
	switch {
	case prevKey == "" && cfg.Auth.JWTPrevKeyID == "":
	case prevKey == "" || cfg.Auth.JWTPrevKeyID == "":
		errs = append(errs, errors.New("JWT_PREVIOUS_KEY_ID and JWT_PREVIOUS_PUBLIC_KEY are set together or not at all"))
	case cfg.Auth.JWTPrevKeyID == cfg.Auth.JWTKeyID:
		errs = append(errs, errors.New("JWT_PREVIOUS_KEY_ID must differ from JWT_KEY_ID"))
	default:
		cfg.Auth.JWTPrevKey = decodeKey("JWT_PREVIOUS_PUBLIC_KEY", ed25519.PublicKeySize, true)
	}

	switch key := cfg.Restate.IdentityKey; {
	case key == "" && cfg.Env == EnvProd:
		errs = append(errs, errors.New("RESTATE_IDENTITY_KEY is required with APP_ENV=prod; make run adds it to .env"))
	case key != "" && !strings.HasPrefix(key, "publickeyv1_"):
		errs = append(errs, errors.New("RESTATE_IDENTITY_KEY must start with publickeyv1_"))
	default:
		// Unset outside prod, or set and well formed.
	}

	// A carrier live needs all its settings; fake and off take none, so a
	// stack never half-points at a real carrier.
	c := cfg.Carrier
	for _, sw := range []struct {
		key, mode string
		settings  map[string]string
	}{
		{"CARRIER_GHTK", c.GHTKMode, map[string]string{
			"GHTK_API_URL": c.GHTKURL, "GHTK_PARTNER_TOKEN": c.GHTKPartner, "GHTK_CLIENT_SOURCE": c.GHTKSource, "GHTK_WEBHOOK_SECRET": c.GHTKWebhook,
		}},
		{"CARRIER_GHN", c.GHNMode, map[string]string{"GHN_API_URL": c.GHNURL, "GHN_PARTNER_TOKEN": c.GHNPartner, "GHN_WEBHOOK_SECRET": c.GHNWebhook}},
	} {
		for key, v := range sw.settings {
			switch {
			case sw.mode == "live" && v == "":
				errs = append(errs, fmt.Errorf("%s is required with %s=live", key, sw.key))
			case sw.mode != "live" && v != "":
				errs = append(errs, fmt.Errorf("%s is set only with %s=live", key, sw.key))
			default:
			}
		}
	}

	if cfg.Email.Sender == "resend" && cfg.Email.ResendAPIKey == "" {
		errs = append(errs, errors.New("RESEND_API_KEY is required with EMAIL_SENDER=resend"))
	}

	return cfg, errors.Join(errs...)
}

// isHostName reports whether s is a DNS host name: dot-separated labels of
// 1 to 63 letters, digits and inner hyphens, at most 253 characters, with no
// scheme, port or path.
func isHostName(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
