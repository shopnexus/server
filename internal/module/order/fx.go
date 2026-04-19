package order

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	restateclient "shopnexus-server/internal/infras/restate"
	orderbiz "shopnexus-server/internal/module/order/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	orderecho "shopnexus-server/internal/module/order/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the order module dependencies.
var Module = fx.Module("order",
	fx.Provide(
		NewOrderStorage,
		orderbiz.NewOrderHandler,
		NewOrderBiz,
		NewRestateClient,
		orderbiz.NewPaymentLock,
		orderbiz.NewRefundLock,
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

// NewOrderBiz creates a Restate-backed client for the order module.
func NewOrderBiz(cfg *config.Config) orderbiz.OrderBiz {
	return orderbiz.NewOrderRestateClient(cfg.Restate.IngressAddress)
}

// NewRestateClient creates a Restate HTTP client for VO calls from transport layer.
func NewRestateClient(cfg *config.Config) *restateclient.Client {
	return restateclient.NewClient(cfg.Restate.IngressAddress)
}
