package config

import (
	"log/slog"
	"strings"
	"testing"
)

func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func required() map[string]string {
	return map[string]string{
		"DATABASE_URL":        "postgres://db",
		"RESTATE_INGRESS_URL": "http://restate:8080",
		"RESTATE_ADMIN_URL":   "http://restate:9070",
	}
}

func TestLoadAllSet(t *testing.T) {
	env := map[string]string{
		"APP_ENV":                     "dev",
		"HTTP_ADDR":                   ":9090",
		"LOG_LEVEL":                   "debug",
		"LOG_FORMAT":                  "text",
		"DATABASE_URL":                "postgres://db",
		"RESTATE_INGRESS_URL":         "http://restate:8080",
		"RESTATE_ADMIN_URL":           "http://restate:9070",
		"RESTATE_LISTEN_ADDR":         "127.0.0.1:9999",
		"RESTATE_ENDPOINT_URL":        "http://host:9999",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otel:4318",
		"SERVICE_NAME":                "svc",
	}
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
	}
	if got != want {
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
		got.Restate.ListenAddr != "127.0.0.1:9080" || got.Restate.EndpointURL != "" {
		t.Fatalf("defaults: %+v", got)
	}
}

func TestLoadListsEveryMissingVariable(t *testing.T) {
	_, err := Load(lookup(nil))
	if err == nil {
		t.Fatal("want error")
	}
	for _, name := range []string{"DATABASE_URL", "RESTATE_INGRESS_URL", "RESTATE_ADMIN_URL"} {
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
