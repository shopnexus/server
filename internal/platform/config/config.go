// Package config reads the process configuration from environment variables,
// once, at start.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
)

// Config holds every setting the server reads. Modules receive the parts they
// need through their constructors and never read the environment themselves.
type Config struct {
	Env          string     // APP_ENV: dev | test | prod
	HTTPAddr     string     // HTTP_ADDR
	LogLevel     slog.Level // LOG_LEVEL: debug | info | warn | error
	LogFormat    string     // LOG_FORMAT: text | json
	DatabaseURL  string     // DATABASE_URL
	NATSURL      string     // NATS_URL
	OTLPEndpoint string     // OTEL_EXPORTER_OTLP_ENDPOINT; empty disables export
	ServiceName  string     // SERVICE_NAME
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

	cfg := Config{
		Env:          oneOf("APP_ENV", "prod", "dev", "test", "prod"),
		HTTPAddr:     get("HTTP_ADDR", ":8080"),
		LogFormat:    oneOf("LOG_FORMAT", "json", "text", "json"),
		DatabaseURL:  need("DATABASE_URL"),
		NATSURL:      need("NATS_URL"),
		OTLPEndpoint: get("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		ServiceName:  get("SERVICE_NAME", "server2"),
	}

	level := oneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error")
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		cfg.LogLevel = slog.LevelInfo
	}

	return cfg, errors.Join(errs...)
}
