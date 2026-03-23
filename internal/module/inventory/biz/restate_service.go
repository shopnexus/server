package inventorybiz

import (
	restate "github.com/restatedev/sdk-go"

	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// InventoryBizService wraps InventoryBiz for Restate registration.
// All methods accept restate.Context and delegate to InventoryBiz with context.Context.
type InventoryBizService struct {
	biz *InventoryBiz
}

func NewInventoryBizService(biz *InventoryBiz) *InventoryBizService {
	return &InventoryBizService{biz: biz}
}

// Stock

func (s *InventoryBizService) GetStock(ctx restate.Context, params GetStockParams) (inventorydb.InventoryStock, error) {
	return s.biz.GetStock(ctx, params)
}

func (s *InventoryBizService) ListStock(ctx restate.Context, params ListStockParams) (sharedmodel.PaginateResult[inventorydb.InventoryStock], error) {
	return s.biz.ListStock(ctx, params)
}

func (s *InventoryBizService) CreateStock(ctx restate.Context, params CreateStockParams) (inventorydb.InventoryStock, error) {
	return s.biz.CreateStock(ctx, params)
}

// Stock History

func (s *InventoryBizService) ListStockHistory(ctx restate.Context, params ListStockHistoryParams) (sharedmodel.PaginateResult[inventorydb.InventoryStockHistory], error) {
	return s.biz.ListStockHistory(ctx, params)
}

// Import

func (s *InventoryBizService) ImportStock(ctx restate.Context, params ImportStockParams) error {
	return s.biz.ImportStock(ctx, params)
}

// Reserve

func (s *InventoryBizService) ReserveInventory(ctx restate.Context, params ReserveInventoryParams) ([]ReserveInventoryResult, error) {
	return s.biz.ReserveInventory(ctx, params)
}

// Serial

func (s *InventoryBizService) UpdateSerial(ctx restate.Context, params UpdateSerialParams) error {
	return s.biz.UpdateSerial(ctx, params)
}

func (s *InventoryBizService) ListSerial(ctx restate.Context, params ListSerialParams) (sharedmodel.PaginateResult[inventorydb.InventorySerial], error) {
	return s.biz.ListSerial(ctx, params)
}

// Most Taken

func (s *InventoryBizService) ListMostTakenSku(ctx restate.Context, params ListMostTakenSkuParams) ([]inventorydb.InventoryStock, error) {
	return s.biz.ListMostTakenSku(ctx, params)
}
