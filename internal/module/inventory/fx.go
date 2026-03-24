package inventory

import (
	"go.uber.org/fx"

	"shopnexus-server/config"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	inventoryecho "shopnexus-server/internal/module/inventory/transport/echo"
	"shopnexus-server/internal/shared/pgsqlc"
)

// Module provides the inventory module dependencies
var Module = fx.Module("inventory",
	fx.Provide(
		NewInventoryStorage,
		inventorybiz.NewInventoryBiz,
		NewInventoryClient,
	),
	fx.Invoke(
		inventoryecho.NewHandler,
	),
)

// NewInventoryStorage creates a new inventory storage backed by PostgreSQL.
func NewInventoryStorage(pool pgsqlc.TxBeginner) inventorybiz.InventoryStorage {
	return pgsqlc.NewStorage(pool, inventorydb.New(pool))
}

// NewInventoryClient creates a Restate-backed client for the inventory module.
func NewInventoryClient(cfg *config.Config) inventorybiz.InventoryClient {
	return inventorybiz.NewInventoryBizRestateClient(cfg.Restate.IngressAddress)
}
