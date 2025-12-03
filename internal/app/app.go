package app

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/bytedance/sonic"
	"go.uber.org/fx"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/cachestruct"
	"shopnexus-remastered/internal/infras/pubsub"
	"shopnexus-remastered/internal/module/account"
	"shopnexus-remastered/internal/module/analytic"
	"shopnexus-remastered/internal/module/catalog"
	"shopnexus-remastered/internal/module/common"
	"shopnexus-remastered/internal/module/inventory"
	"shopnexus-remastered/internal/module/order"
	"shopnexus-remastered/internal/module/promotion"
	"shopnexus-remastered/internal/module/system"
)

// Module combines all internal modules
var Module = fx.Module("main",
	// Infrastructure
	fx.Provide(
		NewConfig,
		NewPgSqlc,
		NewEcho,
		NewCacheStruct,
		NewPubsubClient,
	),

	// Business modules
	common.Module,
	account.Module,
	catalog.Module,
	inventory.Module,
	order.Module,
	promotion.Module,
	analytic.Module,
	system.Module,

	// HTTP server
	fx.Invoke(
		SetupLogger,
		SetupEcho,
		StartHTTPServer,
	),
)

// NewConfig provides the application configuration
func NewConfig() *config.Config {
	return config.GetConfig()
}

func NewCacheStruct() (cachestruct.Client, error) {
	addr := fmt.Sprintf("%s:%s", config.GetConfig().Redis.Host, config.GetConfig().Redis.Port)
	return cachestruct.NewRedisStructClient(cachestruct.RedisConfig{
		Config: cachestruct.Config{
			Decoder: sonic.Unmarshal,
			Encoder: sonic.Marshal,
		},
		Addr:     []string{addr},
		Password: config.GetConfig().Redis.Password,
		DB:       config.GetConfig().Redis.DB,
	})
}

func SetupLogger() {
	cfg := config.GetConfig().Log

	var level slog.Level
	switch cfg.Level {
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
		AddSource: cfg.AddSource,
	})))
}

func NewPubsubClient() (pubsub.Client, error) {
	return pubsub.NewKafkaClient(pubsub.KafkaConfig{
		Config: pubsub.Config{
			Timeout: 10,
			Brokers: []string{"localhost:9092"},
			Decoder: sonic.Unmarshal,
			Encoder: sonic.Marshal,
		},
	})
}
