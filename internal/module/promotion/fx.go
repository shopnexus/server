package promotion

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"go.uber.org/fx"

	"shopnexus-server/internal/infras/cache"
	"shopnexus-server/internal/infras/pg"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	promotionconfig "shopnexus-server/internal/module/promotion/config"
	promotiondb "shopnexus-server/internal/module/promotion/db/sqlc"
	promotionecho "shopnexus-server/internal/module/promotion/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the promotion module dependencies. The pool/cache/logger
// providers are fx.Private — each is constructed from THIS module's own
// Postgres/Redis/Log config and is invisible to other modules' fx graphs,
// so 8 modules can each `Provide(... pgsqlc.TxBeginner ...)` without
// colliding.
var Module = fx.Module("promotion",
	fx.Provide(
		NewPool,
		NewCache,
		NewLogger,
		fx.Private,
	),
	fx.Provide(
		promotionconfig.NewConfig,
		NewPromotionStorage,
		promotionbiz.NewPromotionHandler,
		NewPromotionBiz,
		promotionecho.NewHandler,
	),
	fx.Invoke(
		promotionecho.NewHandler,
	),
)

func NewPool(cfg *promotionconfig.Config, lc fx.Lifecycle) (pgsqlc.TxBeginner, error) {
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

func NewCache(cfg *promotionconfig.Config) (cache.Client, error) {
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

func NewLogger(cfg *promotionconfig.Config) *slog.Logger {
	return buildLogger(cfg.Log.Level, cfg.Log.AddSource, "promotion")
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

// NewPromotionStorage creates a new promotion storage backed by PostgreSQL.
func NewPromotionStorage(pool pgsqlc.TxBeginner) promotionbiz.PromotionStorage {
	return pgsqlc.NewStorage(pool, promotiondb.New(pool))
}

// NewPromotionBiz creates a Restate-backed client for the promotion module.
func NewPromotionBiz(cfg *promotionconfig.Config) promotionbiz.PromotionBiz {
	return promotionbiz.NewPromotionRestateClient(cfg.Restate.IngressAddress)
}
