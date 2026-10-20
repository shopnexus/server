package config

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"log/slog"
	"maps"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

var testKeys = NewAuthKeys()

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func required() map[string]string {
	return map[string]string{
		"DATABASE_URL":        "postgres://db",
		"RESTATE_INGRESS_URL": "http://restate:8080",
		"RESTATE_ADMIN_URL":   "http://restate:9070",
		"OTP_KEY":             b64(testKeys.OTPKey),
		"JWT_KEY_ID":          testKeys.JWTKeyID,
		"JWT_PRIVATE_KEY":     b64(testKeys.JWTKey.Seed()),
	}
}

func TestLoadAllSet(t *testing.T) {
	prev := NewAuthKeys()
	env := required()
	maps.Copy(env, map[string]string{
		"APP_ENV":                     "dev",
		"HTTP_ADDR":                   ":9090",
		"LOG_LEVEL":                   "debug",
		"LOG_FORMAT":                  "text",
		"TRUSTED_PROXIES":             "10.0.0.0/8, 192.168.1.7/32",
		"JWT_PREVIOUS_KEY_ID":         "old",
		"JWT_PREVIOUS_PUBLIC_KEY":     b64(prev.JWTKey.Public().(ed25519.PublicKey)),
		"JWT_ISSUER":                  "iss",
		"JWT_AUDIENCE":                "aud",
		"RESTATE_LISTEN_ADDR":         "127.0.0.1:9999",
		"RESTATE_ENDPOINT_URL":        "http://host:9999",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel:4318",
		"SERVICE_NAME":                "svc",
	})
	got, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Env:         "dev",
		HTTPAddr:    ":9090",
		LogLevel:    slog.LevelDebug,
		LogFormat:   "text",
		DatabaseURL: "postgres://db",
		Restate: RestateConfig{
			IngressURL: "http://restate:8080", AdminURL: "http://restate:9070",
			ListenAddr: "127.0.0.1:9999", EndpointURL: "http://host:9999",
		},
		OTLPEndpoint: "http://otel:4318",
		ServiceName:  "svc",
		TrustedProxies: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.7/32"),
		},
		Auth: AuthConfig{
			OTPKey:       testKeys.OTPKey,
			JWTKeyID:     testKeys.JWTKeyID,
			JWTKey:       testKeys.JWTKey,
			JWTPrevKeyID: "old",
			JWTPrevKey:   prev.JWTKey.Public().(ed25519.PublicKey),
			Issuer:       "iss",
			Audience:     "aud",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	got, err := Load(lookup(required()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.HTTPAddr != ":8080" || got.LogLevel != slog.LevelInfo || got.LogFormat != "json" ||
		got.ServiceName != "server2" || got.Env != "prod" || got.OTLPEndpoint != "" ||
		got.Restate.ListenAddr != "127.0.0.1:9080" || got.Restate.EndpointURL != "" ||
		got.Auth.Issuer != "server2" || got.Auth.Audience != "shopnexus-app" || got.TrustedProxies != nil {
		t.Fatalf("defaults: %+v", got)
	}
}

func TestLoadListsEveryMissingVariable(t *testing.T) {
	_, err := Load(lookup(nil))
	if err == nil {
		t.Fatal("want error")
	}
	for _, name := range []string{"DATABASE_URL", "RESTATE_INGRESS_URL", "RESTATE_ADMIN_URL", "OTP_KEY", "JWT_KEY_ID", "JWT_PRIVATE_KEY"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	cases := []struct {
		key, value string
		mentions   []string
	}{
		{"LOG_LEVEL", "loud", []string{"LOG_LEVEL", "debug", "info", "warn", "error"}},
		{"LOG_FORMAT", "xml", []string{"LOG_FORMAT"}},
		{"APP_ENV", "staging", []string{"APP_ENV"}},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			env := required()
			env[c.key] = c.value
			_, err := Load(lookup(env))
			if err == nil {
				t.Fatal("want error")
			}
			for _, m := range c.mentions {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("error %q does not mention %q", err, m)
				}
			}
		})
	}
}

func TestLoadRejectsBadKeys(t *testing.T) {
	cases := []struct {
		name    string
		set     map[string]string
		mention string
	}{
		{"short otp key", map[string]string{"OTP_KEY": b64(make([]byte, 16))}, "OTP_KEY"},
		{"otp key not base64", map[string]string{"OTP_KEY": "%%%"}, "OTP_KEY"},
		{"seed of the wrong size", map[string]string{"JWT_PRIVATE_KEY": b64(make([]byte, 31))}, "JWT_PRIVATE_KEY"},
		{"previous id without key", map[string]string{"JWT_PREVIOUS_KEY_ID": "old"}, "JWT_PREVIOUS_PUBLIC_KEY"},
		{"previous key without id", map[string]string{"JWT_PREVIOUS_PUBLIC_KEY": b64(make([]byte, 32))}, "JWT_PREVIOUS_KEY_ID"},
		{"previous id equals current", map[string]string{
			"JWT_PREVIOUS_KEY_ID": testKeys.JWTKeyID, "JWT_PREVIOUS_PUBLIC_KEY": b64(make([]byte, 32)),
		}, "must differ"},
		{"bad proxy", map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8,nope"}, "TRUSTED_PROXIES"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := required()
			maps.Copy(env, c.set)
			if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), c.mention) {
				t.Fatalf("err %v, want a mention of %q", err, c.mention)
			}
		})
	}
}

func TestNewAuthKeysRoundTrip(t *testing.T) {
	keys := NewAuthKeys()
	env := required()
	for line := range strings.Lines(keys.Env()) {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		env[k] = v
	}
	got, err := Load(lookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Auth.OTPKey, keys.OTPKey) || !got.Auth.JWTKey.Equal(keys.JWTKey) || got.Auth.JWTKeyID != keys.JWTKeyID {
		t.Fatalf("got %+v", got.Auth)
	}
}
