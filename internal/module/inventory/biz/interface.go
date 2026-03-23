package inventorybiz

import (
	"context"

	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// InventoryClient is the client interface for InventoryBiz, which is used by other modules to call InventoryBiz methods.
//
//go:generate go run shopnexus-server/cmd/genrestate -interface InventoryClient -service InventoryBiz
type InventoryClient interface {
	// Stock
	GetStock(ctx context.Context, params GetStockParams) (inventorydb.InventoryStock, error)
	ListStock(ctx context.Context, params ListStockParams) (sharedmodel.PaginateResult[inventorydb.InventoryStock], error)
	CreateStock(ctx context.Context, params CreateStockParams) (inventorydb.InventoryStock, error)

	// Stock History
	ListStockHistory(ctx context.Context, params ListStockHistoryParams) (sharedmodel.PaginateResult[inventorydb.InventoryStockHistory], error)

	// Import
	ImportStock(ctx context.Context, params ImportStockParams) error

	// Reserve
	ReserveInventory(ctx context.Context, params ReserveInventoryParams) ([]ReserveInventoryResult, error)

	// Serial
	UpdateSerial(ctx context.Context, params UpdateSerialParams) error
	ListSerial(ctx context.Context, params ListSerialParams) (sharedmodel.PaginateResult[inventorydb.InventorySerial], error)

	// Most Taken
	ListMostTakenSku(ctx context.Context, params ListMostTakenSkuParams) ([]inventorydb.InventoryStock, error)
}
