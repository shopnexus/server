package inventory

import (
	"go.uber.org/fx"

	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	inventoryecho "shopnexus-remastered/internal/module/inventory/transport/echo"
	"shopnexus-remastered/internal/shared/pgsqlc"
)

// Module provides the inventory module dependencies
var Module = fx.Module("inventory",
	fx.Provide(
		NewInventoryStorage,
		inventorybiz.NewInventoryBiz,
		inventoryecho.NewHandler,
	),
	fx.Invoke(
		inventoryecho.NewHandler,
	),
)

func NewInventoryStorage(pool pgsqlc.TxBeginner) inventorybiz.InventoryStorage {
	return pgsqlc.NewStorage(pool, inventorydb.New(pool))
}
