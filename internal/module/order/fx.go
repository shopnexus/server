package order

import (
	"go.uber.org/fx"

	orderbiz "shopnexus-remastered/internal/module/order/biz"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	orderecho "shopnexus-remastered/internal/module/order/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

// Module provides the order module dependencies
var Module = fx.Module("order",
	fx.Provide(
		NewOrderStorage,
		orderbiz.NewOrderBiz,
		orderecho.NewHandler,
	),
	fx.Invoke(
		orderecho.NewHandler,
	),
)

func NewOrderStorage(pool pgsqlc.TxBeginner) orderbiz.OrderStorage {
	return pgsqlc.NewStorage(pool, orderdb.New(pool))
}
