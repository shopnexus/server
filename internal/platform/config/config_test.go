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

var testStaffKey = NewStaffKey()

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func lookup(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

// required sets every variable, the ones that may be empty to the empty
// string, for APP_ENV=prod.
func required() map[string]string {
	return map[string]string{
		"APP_ENV":                      "prod",
		"HTTP_ADDR":                    ":8080",
		"LOG_LEVEL":                    "info",
		"LOG_FORMAT":                   "json",
		"DATABASE_URL":                 "postgres://db",
		"OTEL_EXPORTER_OTLP_ENDPOINT":  "",
		"SERVICE_NAME":                 "server2",
		"TRUSTED_PROXIES":              "",
		"RESTATE_INGRESS_URL":          "http://restate:8080",
		"RESTATE_ADMIN_URL":            "http://restate:9070",
		"RESTATE_LISTEN_ADDR":          "127.0.0.1:9080",
		"RESTATE_ENDPOINT_URL":         "",
		"RESTATE_SERVICE_PREFIX":       "",
		"RESTATE_IDENTITY_KEY":         "publickeyv1_test",
		"OTP_KEY":                      b64(testKeys.OTPKey),
		"JWT_KEY_ID":                   testKeys.JWTKeyID,
		"JWT_PRIVATE_KEY":              b64(testKeys.JWTKey.Seed()),
		"JWT_PREVIOUS_KEY_ID":          "",
		"JWT_PREVIOUS_PUBLIC_KEY":      "",
		"JWT_ISSUER":                   "server2",
		"JWT_AUDIENCE":                 "shopnexus-app",
		"STAFF_KEY":                    b64(testStaffKey),
		"STAFF_CONSOLE_ORIGIN":         "http://console.test",
		"OBJECT_STORE_ENDPOINT":        "http://rustfs:9000",
		"OBJECT_STORE_PUBLIC_ENDPOINT": "https://photos.example.test",
		"OBJECT_STORE_REGION":          "us-east-1",
		"OBJECT_STORE_ACCESS_KEY":      "access",
		"OBJECT_STORE_SECRET_KEY":      "secret",
		"OBJECT_STORE_BUCKET":          "server2",
		"MEDIA_PUBLIC_URL":             "http://rustfs:9000/server2",
		"LLM_BASE_URL":                 "https://llm.example.test/v1",
		"LLM_API_KEY":                  "llm-key",
		"LLM_MODEL":                    "a-model",
		"FCM_CREDENTIALS":              "",
		"FCM_PROJECT_ID":               "",
	}
}

func TestLoadAllSet(t *testing.T) {
	t.Parallel()
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
			ListenAddr: "127.0.0.1:9999", EndpointURL: "http://host:9999", IdentityKey: "publickeyv1_test",
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
		Staff: StaffConfig{Key: testStaffKey, ConsoleOrigin: "http://console.test"},
		ObjectStore: ObjectStoreConfig{
			Endpoint: "http://rustfs:9000", PublicEndpoint: "https://photos.example.test", Region: "us-east-1",
			AccessKey: "access", SecretKey: "secret", Bucket: "server2", PublicURL: "http://rustfs:9000/server2",
		},
		LLM: LLMConfig{BaseURL: "https://llm.example.test/v1", APIKey: "llm-key", Model: "a-model"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadAcceptsExplicitEmpty(t *testing.T) {
	t.Parallel()
	got, err := Load(lookup(required()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.OTLPEndpoint != "" || got.TrustedProxies != nil || got.Restate.EndpointURL != "" ||
		got.Restate.ServicePrefix != "" || got.Auth.JWTPrevKeyID != "" || got.Auth.JWTPrevKey != nil {
		t.Fatalf("empty variables: %+v", got)
	}
}

// No variable has a default: leaving any one out names it.
func TestLoadNamesEveryUnsetVariable(t *testing.T) {
	t.Parallel()
	_, err := Load(lookup(nil))
	if err == nil {
		t.Fatal("want error")
	}
	for name := range required() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
	for name := range required() {
		env := required()
		delete(env, name)
		if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), name+" is not set") {
			t.Errorf("without %s: %v", name, err)
		}
	}
}

func TestLoadRejectsEmptyRequired(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"HTTP_ADDR", "DATABASE_URL", "SERVICE_NAME", "RESTATE_LISTEN_ADDR", "JWT_ISSUER", "JWT_AUDIENCE", "STAFF_CONSOLE_ORIGIN"} {
		env := required()
		env[name] = ""
		if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), name+" is empty") {
			t.Errorf("empty %s: %v", name, err)
		}
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
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
		{"short staff key", map[string]string{"STAFF_KEY": b64(make([]byte, 16))}, "STAFF_KEY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			env := required()
			maps.Copy(env, c.set)
			if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), c.mention) {
				t.Fatalf("err %v, want a mention of %q", err, c.mention)
			}
		})
	}
}

func TestNewAuthKeysRoundTrip(t *testing.T) {
	t.Parallel()
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

// Production refuses unsigned Restate requests, so it needs the key.
func TestLoadProdNeedsTheRestateIdentityKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"", "not-a-key"} {
		env := required()
		env["RESTATE_IDENTITY_KEY"] = key
		if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "RESTATE_IDENTITY_KEY") {
			t.Fatalf("%q: %v", key, err)
		}
	}
	env := required()
	env["APP_ENV"], env["RESTATE_IDENTITY_KEY"] = "dev", ""
	if _, err := Load(lookup(env)); err != nil {
		t.Fatalf("dev without the key: %v", err)
	}
}

// Push credentials need their project; empty both means the log sender.
func TestLoadPush(t *testing.T) {
	t.Parallel()
	env := required()
	env["FCM_CREDENTIALS"] = b64([]byte(`{"type":"service_account"}`))
	if _, err := Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "FCM_PROJECT_ID") {
		t.Fatalf("credentials without a project: %v", err)
	}
	env["FCM_PROJECT_ID"] = "shopnexus"
	got, err := Load(lookup(env))
	if err != nil || string(got.Push.Credentials) != `{"type":"service_account"}` || got.Push.ProjectID != "shopnexus" {
		t.Fatalf("%+v %v", got.Push, err)
	}
	env["FCM_CREDENTIALS"] = "%%%"
	if _, err = Load(lookup(env)); err == nil || !strings.Contains(err.Error(), "FCM_CREDENTIALS") {
		t.Fatalf("not base64: %v", err)
	}
}
