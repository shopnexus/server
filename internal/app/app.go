package app

import (
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/logger"
	"shopnexus-remastered/internal/module/account"
	"shopnexus-remastered/internal/module/auth"
	"shopnexus-remastered/internal/module/catalog"
	"shopnexus-remastered/internal/module/order"
	"shopnexus-remastered/internal/module/promotion"
	"shopnexus-remastered/internal/module/shared"

	"go.uber.org/fx"
)

// Module combines all internal modules
var Module = fx.Module("main",
	// Infrastructure
	fx.Provide(
		NewConfig,
		NewDatabase,
		NewEcho,
	),

	// Business modules
	shared.Module,
	account.Module,
	auth.Module,
	catalog.Module,
	order.Module,
	promotion.Module,

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

func SetupLogger() {
	logger.InitLogger()
}
