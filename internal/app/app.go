package app

import (
	"log/slog"
	"os"

	"go.uber.org/fx"

	appconfig "shopnexus-server/internal/app/config"
	"shopnexus-server/internal/infras/ratelimit"
	"shopnexus-server/internal/module/account"
	"shopnexus-server/internal/module/analytic"
	"shopnexus-server/internal/module/catalog"
	"shopnexus-server/internal/module/chat"
	"shopnexus-server/internal/module/common"
	"shopnexus-server/internal/module/inventory"
	"shopnexus-server/internal/module/order"
	"shopnexus-server/internal/module/promotion"

	"shopnexus-server/internal/provider/geocoding"
)

// Module composes the application root. Modules each own their own pool/cache/
// logger via fx.Private; app/ no longer carries shared infra. What remains is
// process-level: app config (port + log default + restate registration), the
// HTTP server, geocoding (no consumer-specific config).
var Module = fx.Module("main",
	fx.Provide(
		appconfig.NewConfig,
		NewEcho,
		NewGeocodingProvider,
		NewRateLimiter,
	),

	common.Module,
	account.Module,
	catalog.Module,
	inventory.Module,
	order.Module,
	promotion.Module,
	analytic.Module,
	chat.Module,

	fx.Invoke(
		SetupLogger,
		SetupRestate,
		SetupEcho,
		SetupHTTPServer,
	),
)

// SetupLogger sets the process-wide slog.Default. Module-scoped loggers are
// constructed inside each module's fx.Module from its own log config.
func SetupLogger(cfg *appconfig.Config) {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.Log.AddSource,
	})))
}

func NewGeocodingProvider() geocoding.Client {
	return geocoding.NewNominatimProvider()
}

func NewRateLimiter() *ratelimit.Factory {
	return ratelimit.NewFactory(nil)
}
