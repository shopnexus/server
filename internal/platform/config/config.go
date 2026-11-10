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
	"net/netip"
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

// Config holds every setting the server reads. Modules receive the parts they
// need through their constructors and never read the environment themselves.
type Config struct {
	Env            Env
	HTTPAddr       string     // HTTP_ADDR
	LogLevel       slog.Level // LOG_LEVEL: debug | info | warn | error
	LogFormat      LogFormat
	DatabaseURL    string // DATABASE_URL
	OTLPEndpoint   string // OTEL_EXPORTER_OTLP_ENDPOINT; empty disables export
	ServiceName    string // SERVICE_NAME
	Restate        RestateConfig
	TrustedProxies []netip.Prefix // TRUSTED_PROXIES: CIDRs whose X-Forwarded-For is believed
	Auth           AuthConfig
	Console        ConsoleConfig
	ObjectStore    ObjectStoreConfig
	LLM            LLMConfig
	Embed          EmbedConfig
	Push           PushConfig
	Firebase       FirebaseConfig
	Email          EmailConfig
	Google         GoogleConfig
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

// EmailConfig picks how the server mails sign-in codes.
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
}

// EmbedConfig says where the embedding models are and how search uses them.
// In the cluster both base URLs are the stack's LiteLLM and the models are
// its aliases; locally they are TEI and ai, which take no key.
type EmbedConfig struct {
	TextBaseURL  string // EMBED_TEXT_BASE_URL: POST {base}/embeddings for text
	ImageBaseURL string // EMBED_IMAGE_BASE_URL: the same for photos and visual text
	TextModel    string // EMBED_TEXT_MODEL, the alias embed-text
	ImageModel   string // EMBED_IMAGE_MODEL, the alias embed-image
	// TextVersion and ImageVersion name the model behind each alias. Every
	// vector row stores its version; a new version embeds again.
	TextVersion  string        // EMBED_TEXT_VERSION
	ImageVersion string        // EMBED_IMAGE_VERSION
	IndexAPIKey  string        // EMBED_INDEX_API_KEY: ListingEmbed's key; empty locally
	SearchAPIKey string        // EMBED_SEARCH_API_KEY: the search's key; empty locally
	Timeout      time.Duration // EMBED_TIMEOUT: one embedding call
	Slots        int           // EMBED_SLOTS: embeddings ListingEmbed runs at once in this process
	// TextMinSimilarity and PhotoMinSimilarity are the cosine a listing needs
	// to join a vector list.
	TextMinSimilarity  float64 // EMBED_TEXT_MIN_SIMILARITY, in (0, 1)
	PhotoMinSimilarity float64 // EMBED_PHOTO_MIN_SIMILARITY, in (0, 1)
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
}

// DashboardApps are the ops dashboards DASHBOARD_HOSTS may name. Module
// console gives each the role that opens it.
var DashboardApps = []string{"grafana", "restate", "litellm", "s3", "argocd"}

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
	fraction := func(key string) float64 {
		v := need(key)
		f, err := strconv.ParseFloat(v, 64)
		if v != "" && (err != nil || f <= 0 || f >= 1) {
			errs = append(errs, fmt.Errorf("%s=%q: want a number between 0 and 1", key, v))
		}
		return f
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
		Env:         Env(oneOf("APP_ENV", string(EnvDev), string(EnvTest), string(EnvProd))),
		HTTPAddr:    need("HTTP_ADDR"),
		LogLevel:    logLevel,
		LogFormat:   LogFormat(oneOf("LOG_FORMAT", string(LogText), string(LogJSON))),
		DatabaseURL: need("DATABASE_URL"),
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
		},
		ObjectStore: ObjectStoreConfig{
			Endpoint:       need("OBJECT_STORE_ENDPOINT"),
			PublicEndpoint: need("OBJECT_STORE_PUBLIC_ENDPOINT"),
			Region:         need("OBJECT_STORE_REGION"),
			AccessKey:      need("OBJECT_STORE_ACCESS_KEY"),
			SecretKey:      need("OBJECT_STORE_SECRET_KEY"),
			Bucket:         need("OBJECT_STORE_BUCKET"),
			PublicURL:      need("MEDIA_PUBLIC_URL"),
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
		LLM:    LLMConfig{BaseURL: need("LLM_BASE_URL"), APIKey: need("LLM_API_KEY"), Model: need("LLM_MODEL")},
		Embed: EmbedConfig{
			TextBaseURL: need("EMBED_TEXT_BASE_URL"), ImageBaseURL: need("EMBED_IMAGE_BASE_URL"),
			TextModel: need("EMBED_TEXT_MODEL"), ImageModel: need("EMBED_IMAGE_MODEL"),
			TextVersion: need("EMBED_TEXT_VERSION"), ImageVersion: need("EMBED_IMAGE_VERSION"),
			IndexAPIKey: set("EMBED_INDEX_API_KEY"), SearchAPIKey: set("EMBED_SEARCH_API_KEY"),
			Timeout: duration("EMBED_TIMEOUT"), Slots: positive("EMBED_SLOTS"),
			TextMinSimilarity: fraction("EMBED_TEXT_MIN_SIMILARITY"), PhotoMinSimilarity: fraction("EMBED_PHOTO_MIN_SIMILARITY"),
		},
		OTLPEndpoint:   set("OTEL_EXPORTER_OTLP_ENDPOINT"),
		ServiceName:    need("SERVICE_NAME"),
		TrustedProxies: proxies,
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
