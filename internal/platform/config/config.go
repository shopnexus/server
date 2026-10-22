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
	"strings"
)

// Config holds every setting the server reads. Modules receive the parts they
// need through their constructors and never read the environment themselves.
type Config struct {
	Env            string     // APP_ENV: dev | test | prod
	HTTPAddr       string     // HTTP_ADDR
	LogLevel       slog.Level // LOG_LEVEL: debug | info | warn | error
	LogFormat      string     // LOG_FORMAT: text | json
	DatabaseURL    string     // DATABASE_URL
	OTLPEndpoint   string     // OTEL_EXPORTER_OTLP_ENDPOINT; empty disables export
	ServiceName    string     // SERVICE_NAME
	Restate        RestateConfig
	TrustedProxies []netip.Prefix // TRUSTED_PROXIES: CIDRs whose X-Forwarded-For is believed
	Auth           AuthConfig
}

// AuthConfig holds the auth module's keys and token settings. No key is ever
// logged.
type AuthConfig struct {
	OTPKey       []byte             // OTP_KEY: base64, at least 32 bytes
	JWTKeyID     string             // JWT_KEY_ID: the kid of JWTKey
	JWTKey       ed25519.PrivateKey // JWT_PRIVATE_KEY: base64 of the 32-byte seed
	JWTPrevKeyID string             // JWT_PREVIOUS_KEY_ID: set during a key change
	JWTPrevKey   ed25519.PublicKey  // JWT_PREVIOUS_PUBLIC_KEY: base64
	Issuer       string             // JWT_ISSUER
	Audience     string             // JWT_AUDIENCE
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
	// ones. Required with APP_ENV=prod; make run writes it to .env.
	IdentityKey string
}

// Load builds a Config from getenv. It reports every missing or invalid
// variable in one error.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	get := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	need := func(key string) string {
		v := getenv(key)
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
		return v
	}
	oneOf := func(key, def string, allowed ...string) string {
		v := get(key, def)
		if !slices.Contains(allowed, v) {
			errs = append(errs, fmt.Errorf("%s=%q: want one of %v", key, v, allowed))
		}
		return v
	}

	decodeKey := func(key string, size int, exact bool) []byte {
		v := getenv(key)
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required; make run adds it to .env", key))
			return nil
		}
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(b) < size || (exact && len(b) != size) {
			want := fmt.Sprintf("at least %d", size)
			if exact {
				want = fmt.Sprint(size)
			}
			errs = append(errs, fmt.Errorf("%s: want base64 of %s bytes", key, want))
			return nil
		}
		return b
	}

	cfg := Config{
		Env:         oneOf("APP_ENV", "prod", "dev", "test", "prod"),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
		LogFormat:   oneOf("LOG_FORMAT", "json", "text", "json"),
		DatabaseURL: need("DATABASE_URL"),
		Restate: RestateConfig{
			IngressURL:    need("RESTATE_INGRESS_URL"),
			AdminURL:      need("RESTATE_ADMIN_URL"),
			ListenAddr:    get("RESTATE_LISTEN_ADDR", "127.0.0.1:9080"),
			EndpointURL:   get("RESTATE_ENDPOINT_URL", ""),
			ServicePrefix: get("RESTATE_SERVICE_PREFIX", ""),
			IdentityKey:   get("RESTATE_IDENTITY_KEY", ""),
		},
		Auth: AuthConfig{
			OTPKey:       decodeKey("OTP_KEY", 32, false),
			JWTKeyID:     need("JWT_KEY_ID"),
			JWTPrevKeyID: getenv("JWT_PREVIOUS_KEY_ID"),
			Issuer:       get("JWT_ISSUER", defaultIssuer),
			Audience:     get("JWT_AUDIENCE", defaultAudience),
		},
		OTLPEndpoint: get("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		ServiceName:  get("SERVICE_NAME", "server2"),
	}

	// oneOf has checked the value, so it parses.
	_ = cfg.LogLevel.UnmarshalText([]byte(oneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error")))

	for s := range strings.SplitSeq(getenv("TRUSTED_PROXIES"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %q is not a CIDR", s))
			continue
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, p.Masked())
	}

	if seed := decodeKey("JWT_PRIVATE_KEY", ed25519.SeedSize, true); seed != nil {
		cfg.Auth.JWTKey = ed25519.NewKeyFromSeed(seed)
	}
	prevKey := getenv("JWT_PREVIOUS_PUBLIC_KEY")
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
	case key == "" && cfg.Env == "prod":
		errs = append(errs, errors.New("RESTATE_IDENTITY_KEY is required with APP_ENV=prod; make run adds it to .env"))
	case key != "" && !strings.HasPrefix(key, "publickeyv1_"):
		errs = append(errs, errors.New("RESTATE_IDENTITY_KEY must start with publickeyv1_"))
	}

	return cfg, errors.Join(errs...)
}
