package common

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"go.uber.org/fx"

	"shopnexus-server/internal/infras/cache"
	"shopnexus-server/internal/infras/pg"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commonconfig "shopnexus-server/internal/module/common/config"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonecho "shopnexus-server/internal/module/common/transport/echo"
	"shopnexus-server/internal/provider/exchange"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the common module dependencies. The pool/cache/logger
// providers are fx.Private — each is constructed from THIS module's own
// Postgres/Redis/Log config and is invisible to other modules' fx graphs,
// so 8 modules can each `Provide(... pgsqlc.TxBeginner ...)` without
// colliding.
var Module = fx.Module("common",
	fx.Provide(
		NewPool,
		NewCache,
		NewLogger,
		fx.Private,
	),
	fx.Provide(
		commonconfig.NewConfig,
		NewCommonStorage,
		NewExchangeClient,
		commonbiz.NewcommonBiz,
		NewCommonBiz,
		commonecho.NewHandler,
	),
	fx.Invoke(
		commonecho.NewHandler,
	),
)

func NewPool(cfg *commonconfig.Config, lc fx.Lifecycle) (pgsqlc.TxBeginner, error) {
	pool, err := pg.New(pg.Options{
		Url:             cfg.Postgres.Url,
		Host:            cfg.Postgres.Host,
		Port:            cfg.Postgres.Port,
		Username:        cfg.Postgres.Username,
		Password:        cfg.Postgres.Password,
		Database:        cfg.Postgres.Database,
		MaxConnections:  cfg.Postgres.MaxConnections,
		MaxConnIdleTime: cfg.Postgres.MaxConnIdleTime,
	})
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error { return pool.Ping(ctx) },
		OnStop:  func(context.Context) error { pool.Close(); return nil },
	})
	return pool, nil
}

func NewCache(cfg *commonconfig.Config) (cache.Client, error) {
	rdb, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{fmt.Sprintf("%s:%s", cfg.Redis.Host, cfg.Redis.Port)},
		Password:    cfg.Redis.Password,
	})
	if err != nil {
		return nil, err
	}
	return cache.NewRedisStructClient(rdb, cache.Config{
		Encoder: sonic.Marshal,
		Decoder: sonic.Unmarshal,
	})
}

func NewLogger(cfg *commonconfig.Config) *slog.Logger {
	return buildLogger(cfg.Log.Level, cfg.Log.AddSource, "common")
}

// buildLogger is the shared module-logger constructor — copied across module
// fx.go files to keep each module fully self-describing (no shared helper).
func buildLogger(levelStr string, addSource bool, module string) *slog.Logger {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: addSource,
	})
	return slog.New(h).With(slog.String("module", module))
}

// NewCommonStorage creates a new common storage backed by PostgreSQL.
func NewCommonStorage(pool pgsqlc.TxBeginner) commonbiz.CommonStorage {
	return pgsqlc.NewStorage(pool, commondb.New(pool))
}

// NewCommonBiz creates a Restate-backed client for the common module.
func NewCommonBiz(cfg *commonconfig.Config) commonbiz.CommonBiz {
	return commonbiz.NewCommonRestateClient(cfg.Restate.IngressAddress)
}

// NewExchangeClient provides a CurrencyAPI-backed exchange.Client
// configured from app settings. Chosen over Frankfurter for full ISO 4217
// coverage (VND, COP, CLP etc. that ECB-based providers don't ship).
func NewExchangeClient(cfg *commonconfig.Config) exchange.Client {
	return exchange.NewCurrencyAPI(
		cfg.Exchange.UpstreamURL,
		cfg.Exchange.APIKey,
		&http.Client{Timeout: cfg.Exchange.HTTPTimeout},
	)
}
