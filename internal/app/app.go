package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bytedance/sonic"
	"go.uber.org/fx"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/cachestruct"
	"shopnexus-remastered/internal/infras/embedding"
	"shopnexus-remastered/internal/infras/milvus"
	"shopnexus-remastered/internal/infras/pubsub"
	"shopnexus-remastered/internal/module/account"
	"shopnexus-remastered/internal/module/analytic"
	"shopnexus-remastered/internal/module/catalog"
	"shopnexus-remastered/internal/module/chat"
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
		NewMilvusClient,
		NewEmbeddingClient,
	),

	// Business modules
	common.Module,
	account.Module,
	catalog.Module,
	inventory.Module,
	order.Module,
	promotion.Module,
	analytic.Module,
	chat.Module,
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

func NewPubsubClient(cfg *config.Config) (pubsub.Client, error) {
	return pubsub.NewKafkaClient(pubsub.KafkaConfig{
		Config: pubsub.Config{
			Timeout: 10,
			Brokers: []string{fmt.Sprintf("%s:%s", cfg.Kafka.Host, cfg.Kafka.Port)},
			Decoder: sonic.Unmarshal,
			Encoder: sonic.Marshal,
		},
	})
}

func NewMilvusClient(lc fx.Lifecycle, cfg *config.Config) (*milvus.Client, error) {
	client, err := milvus.NewClient(context.Background(), milvus.Config{
		Address: cfg.Milvus.Address,
	})
	if err != nil {
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			slog.Info("Closing Milvus connection...")
			return client.Close(ctx)
		},
	})

	return client, nil
}

func NewEmbeddingClient(cfg *config.Config) *embedding.Client {
	return embedding.NewClient(embedding.Config{
		URL: cfg.Embedding.URL,
	})
}
