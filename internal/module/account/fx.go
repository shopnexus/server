package account

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
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountconfig "shopnexus-server/internal/module/account/config"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	accountecho "shopnexus-server/internal/module/account/transport/echo"
	authclaims "shopnexus-server/internal/shared/claims"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the account module dependencies. The pool/cache/logger
// providers are fx.Private — each is constructed from THIS module's own
// Postgres/Redis/Log config and is invisible to other modules' fx graphs,
// so 8 modules can each `Provide(... pgsqlc.TxBeginner ...)` without
// colliding.
var Module = fx.Module("account",
	fx.Provide(
		NewPool,
		NewCache,
		NewLogger,
		fx.Private,
	),
	fx.Provide(
		accountconfig.NewConfig,
		NewAccountStorage,
		accountbiz.NewAccountHandler,
		NewAccountBiz,
		accountecho.NewHandler,
	),
	fx.Invoke(
		accountecho.NewHandler,
		WireClaimsSecret,
	),
)

func NewPool(cfg *accountconfig.Config, lc fx.Lifecycle) (pgsqlc.TxBeginner, error) {
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

func NewCache(cfg *accountconfig.Config) (cache.Client, error) {
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

func NewLogger(cfg *accountconfig.Config) *slog.Logger {
	return buildLogger(cfg.Log.Level, cfg.Log.AddSource, "account")
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

// WireClaimsSecret installs the JWT access-token secret into the shared
// authclaims package so GetClaims(r) (called by every transport handler) can
// validate tokens without an injected dep.
// TODO: nghĩ cách khác để viết về auth
func WireClaimsSecret(cfg *accountconfig.Config) {
	authclaims.SetSecret(cfg.JWT.Secret)
}

// NewAccountStorage creates a new account storage backed by PostgreSQL.
func NewAccountStorage(pool pgsqlc.TxBeginner) accountbiz.AccountStorage {
	return pgsqlc.NewStorage(pool, accountdb.New(pool))
}

// NewAccountBiz creates a Restate-backed client for the account module.
func NewAccountBiz(cfg *accountconfig.Config) accountbiz.AccountBiz {
	return accountbiz.NewAccountRestateClient(cfg.Restate.IngressAddress)
}
