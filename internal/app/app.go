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
	"shopnexus-server/internal/infras/cache"
	"shopnexus-server/internal/infras/milvus"
	"shopnexus-server/internal/infras/pubsub"
	restateclient "shopnexus-server/internal/infras/restate"
	"shopnexus-server/internal/module/account"
	"shopnexus-server/internal/module/analytic"
	"shopnexus-server/internal/module/catalog"
	"shopnexus-server/internal/module/chat"
	"shopnexus-server/internal/module/common"
	"shopnexus-server/internal/module/inventory"
	"shopnexus-server/internal/module/order"
	"shopnexus-server/internal/module/promotion"

	"shopnexus-server/internal/provider/geocoding"
	"shopnexus-server/internal/provider/llm"
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
		NewLLMClient,
		NewRestateClient,
		NewGeocodingProvider,
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

func NewCacheStruct() (cache.Client, error) {
	addr := fmt.Sprintf("%s:%s", config.GetConfig().Redis.Host, config.GetConfig().Redis.Port)
	return cache.NewRedisStructClient(cache.RedisConfig{
		Config: cache.Config{
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

func NewGeocodingProvider() geocoding.Client {
	return geocoding.NewNominatimProvider()
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

func NewLLMClient(cfg *config.Config) (llm.Client, error) {
	switch cfg.LLM.Provider {
	case "python":
		return llm.NewPythonClient(llm.PythonConfig{
			URL: cfg.LLM.Python.URL,
		}), nil
	case "openai":
		return llm.NewOpenAIClient(llm.OpenAIConfig{
			APIKey:     cfg.LLM.OpenAI.APIKey,
			BaseURL:    cfg.LLM.OpenAI.BaseURL,
			EmbedModel: cfg.LLM.OpenAI.EmbedModel,
			ChatModel:  cfg.LLM.OpenAI.ChatModel,
		}), nil
	case "bedrock":
		return llm.NewBedrockClient(context.Background(), llm.BedrockConfig{
			Region:       cfg.LLM.Bedrock.Region,
			EmbedModelID: cfg.LLM.Bedrock.EmbedModelID,
			ChatModelID:  cfg.LLM.Bedrock.ChatModelID,
		})
	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", cfg.LLM.Provider)
	}
}
