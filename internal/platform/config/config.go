// Package config reads the process configuration from environment variables,
// once, at start.
package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strconv"
	"strings"
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
	Staff          StaffConfig
	ObjectStore    ObjectStoreConfig
	LLM            LLMConfig
	Push           PushConfig
	Firebase       FirebaseConfig
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

// LLMConfig says where the model that reads search queries is. The key is
// never logged.
type LLMConfig struct {
	BaseURL string // LLM_BASE_URL: an OpenAI-compatible API, such as https://proxy/v1
	APIKey  string // LLM_API_KEY
	Model   string // LLM_MODEL
}

// AuthConfig holds the auth module's keys and token settings. No key is ever
// logged.
type AuthConfig struct {
	JWTKeyID     string             // JWT_KEY_ID: the kid of JWTKey
	JWTKey       ed25519.PrivateKey // JWT_PRIVATE_KEY: base64 of the 32-byte seed
	JWTPrevKeyID string             //exhaustruct:optional JWT_PREVIOUS_KEY_ID: empty outside a key change
	JWTPrevKey   ed25519.PublicKey  //exhaustruct:optional JWT_PREVIOUS_PUBLIC_KEY: base64, empty with the ID
	Issuer       string             // JWT_ISSUER
	Audience     string             // JWT_AUDIENCE
}

// StaffConfig holds the staff module's key and the console's origin. The key
// is never logged.
type StaffConfig struct {
	Key           []byte // STAFF_KEY: base64, at least 32 bytes; the staff subkeys derive from it
	ConsoleOrigin string // STAFF_CONSOLE_ORIGIN: the web console's origin, the only one CORS allows
}

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
	IngressURL  string // RESTATE_INGRESS_URL
	AdminURL    string // RESTATE_ADMIN_URL
	ListenAddr  string // RESTATE_LISTEN_ADDR: where this process serves its Restate endpoint
	EndpointURL string // RESTATE_ENDPOINT_URL: how Restate reaches it; empty means http://ListenAddr
	// ServicePrefix (RESTATE_SERVICE_PREFIX) goes before every service name,
	// so servers sharing one Restate server, as tests do, keep apart.
	ServicePrefix string
	// IdentityKey (RESTATE_IDENTITY_KEY, "publickeyv1_...") is the public key
	// the Restate server signs requests with; the endpoint refuses unsigned
	// ones. It may be empty outside APP_ENV=prod; make run writes it to .env.
	IdentityKey string
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
		},
		Auth: AuthConfig{
			JWTKeyID:     need("JWT_KEY_ID"),
			JWTKey:       jwtKey,
			JWTPrevKeyID: set("JWT_PREVIOUS_KEY_ID"),
			Issuer:       need("JWT_ISSUER"),
			Audience:     need("JWT_AUDIENCE"),
		},
		Staff: StaffConfig{
			Key:           decodeKey("STAFF_KEY", 32, false),
			ConsoleOrigin: need("STAFF_CONSOLE_ORIGIN"),
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
		LLM:            LLMConfig{BaseURL: need("LLM_BASE_URL"), APIKey: need("LLM_API_KEY"), Model: need("LLM_MODEL")},
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

	return cfg, errors.Join(errs...)
}
