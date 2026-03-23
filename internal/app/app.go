package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/fx"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/cachestruct"
	restateclient "shopnexus-server/internal/infras/restate"
	"shopnexus-server/internal/infras/embedding"
	"shopnexus-server/internal/infras/milvus"
	"shopnexus-server/internal/infras/pubsub"
	"shopnexus-server/internal/module/account"
	"shopnexus-server/internal/module/analytic"
	"shopnexus-server/internal/module/catalog"
	"shopnexus-server/internal/module/chat"
	"shopnexus-server/internal/module/common"
	"shopnexus-server/internal/module/inventory"
	"shopnexus-server/internal/module/order"
	"shopnexus-server/internal/module/promotion"
	"shopnexus-server/internal/module/system"
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
		NewRestateClient,
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
		SetupRestate,
		SetupEcho,
		SetupHTTPServer,
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

func NewRestateClient(cfg *config.Config) *restateclient.Client {
	return restateclient.NewClient(cfg.Restate.IngressAddress)
}

func NewPubsubClient(cfg *config.Config) (pubsub.Client, error) {
	return pubsub.NewNatsClient(pubsub.NatsConfig{
		Config: pubsub.Config{
			Timeout: 10 * time.Second,
			Brokers: []string{fmt.Sprintf("%s:%s", cfg.Nats.Host, cfg.Nats.Port)},
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
