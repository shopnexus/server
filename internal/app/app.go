package app

import (
	"encoding/json"
	"fmt"

	"go.uber.org/fx"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/cachestruct"
	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/logger"
	"shopnexus-remastered/internal/module/account"
	"shopnexus-remastered/internal/module/analytic"
	"shopnexus-remastered/internal/module/auth"
	"shopnexus-remastered/internal/module/catalog"
	"shopnexus-remastered/internal/module/inventory"
	"shopnexus-remastered/internal/module/order"
	"shopnexus-remastered/internal/module/promotion"
	"shopnexus-remastered/internal/module/search"
	"shopnexus-remastered/internal/module/shared"
	"shopnexus-remastered/internal/module/system"
)

// Module combines all internal modules
var Module = fx.Module("main",
	// Infrastructure
	fx.Provide(
		NewConfig,
		NewDatabase,
		NewEcho,
		NewCacheStruct,
		NewPubsubClient,
	),

	// Business modules
	shared.Module,
	account.Module,
	auth.Module,
	catalog.Module,
	inventory.Module,
	order.Module,
	promotion.Module,
	analytic.Module,
	system.Module,
	search.Module,

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
			Decoder: json.Unmarshal,
			Encoder: json.Marshal,
		},
		Addr:     []string{addr},
		Password: config.GetConfig().Redis.Password,
		DB:       config.GetConfig().Redis.DB,
	})
}

func SetupLogger() {
	logger.InitLogger()
}

func NewPubsubClient() (pubsub.Client, error) {
	return pubsub.NewKafkaClient(pubsub.KafkaConfig{
		Config: pubsub.Config{
			Timeout: 10,
			Brokers: []string{"localhost:9092"},
			Decoder: json.Unmarshal,
			Encoder: json.Marshal,
		},
	})
}
