package chat

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
	chatbiz "shopnexus-server/internal/module/chat/biz"
	chatconfig "shopnexus-server/internal/module/chat/config"
	chatdb "shopnexus-server/internal/module/chat/db/sqlc"
	chatecho "shopnexus-server/internal/module/chat/transport/echo"
	commonbiz "shopnexus-server/internal/module/common/biz"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the chat module dependencies. The pool/cache/logger
// providers are fx.Private — each is constructed from THIS module's own
// Postgres/Redis/Log config and is invisible to other modules' fx graphs,
// so 8 modules can each `Provide(... pgsqlc.TxBeginner ...)` without
// colliding.
var Module = fx.Module("chat",
	fx.Provide(
		NewPool,
		NewCache,
		NewLogger,
		fx.Private,
	),
	fx.Provide(
		chatconfig.NewConfig,
		NewChatStorage,
		NewChatHandler,
		NewChatBiz,
		chatecho.NewHandler,
	),
	fx.Invoke(
		chatecho.NewHandler,
	),
)

func NewPool(cfg *chatconfig.Config, lc fx.Lifecycle) (pgsqlc.TxBeginner, error) {
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

func NewCache(cfg *chatconfig.Config) (cache.Client, error) {
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

func NewLogger(cfg *chatconfig.Config) *slog.Logger {
	return buildLogger(cfg.Log.Level, cfg.Log.AddSource, "chat")
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

func NewChatHandler(storage chatbiz.ChatStorage, common commonbiz.CommonBiz) *chatbiz.ChatHandler {
	return chatbiz.NewChatHandler(storage, common)
}

// NewChatStorage creates a new chat storage backed by PostgreSQL.
func NewChatStorage(pool pgsqlc.TxBeginner) chatbiz.ChatStorage {
	return pgsqlc.NewStorage(pool, chatdb.New(pool))
}

// NewChatBiz creates a Restate-backed client for the chat module.
func NewChatBiz(cfg *chatconfig.Config) chatbiz.ChatBiz {
	return chatbiz.NewChatRestateClient(cfg.Restate.IngressAddress)
}
