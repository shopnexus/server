package common

import (
	"net/http"

	"go.uber.org/fx"

	"shopnexus-server/config"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	commonecho "shopnexus-server/internal/module/common/transport/echo"
	"shopnexus-server/internal/provider/exchange"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the common module dependencies.
var Module = fx.Module("common",
	fx.Provide(
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

// NewCommonStorage creates a new common storage backed by PostgreSQL.
func NewCommonStorage(pool pgsqlc.TxBeginner) commonbiz.CommonStorage {
	return pgsqlc.NewStorage(pool, commondb.New(pool))
}

// NewCommonBiz creates a Restate-backed client for the common module.
func NewCommonBiz(cfg *config.Config) commonbiz.CommonBiz {
	return commonbiz.NewCommonRestateClient(cfg.Restate.IngressAddress)
}

// NewExchangeClient provides a CurrencyAPI-backed exchange.Client
// configured from app settings. Chosen over Frankfurter for full ISO 4217
// coverage (VND, COP, CLP etc. that ECB-based providers don't ship).
func NewExchangeClient(cfg *config.Config) exchange.Client {
	return exchange.NewCurrencyAPI(
		cfg.App.Exchange.UpstreamURL,
		cfg.App.Exchange.APIKey,
		&http.Client{Timeout: cfg.App.Exchange.HTTPTimeout},
	)
}
