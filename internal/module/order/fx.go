package order

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	orderecho "shopnexus-server/internal/module/order/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the order module dependencies
var Module = fx.Module("order",
	fx.Provide(
		NewOrderStorage,
		orderbiz.NewOrderBiz,
		NewOrderClient,
		orderecho.NewHandler,
	),
	fx.Invoke(
		orderecho.NewHandler,
	),
)

// NewOrderStorage creates a new order storage backed by PostgreSQL.
func NewOrderStorage(pool pgsqlc.TxBeginner) orderbiz.OrderStorage {
	return pgsqlc.NewStorage(pool, orderdb.New(pool))
}

// NewOrderClient creates a Restate-backed client for the order module.
func NewOrderClient(cfg *config.Config) orderbiz.OrderClient {
	return orderbiz.NewOrderBizProxy(cfg.Restate.IngressAddress)
}
