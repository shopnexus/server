package inventorybiz

import (
	restate "github.com/restatedev/sdk-go"

	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	inventorymodel "shopnexus-server/internal/module/inventory/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// getStockByRef is a shared helper to look up stock by (ref_type, ref_id).
func (b *InventoryHandler) getStockByRef(ctx restate.Context, q inventorydb.Querier, refType inventorydb.InventoryStockRefType, refID uuid.UUID) (inventorydb.InventoryStock, error) {
	return q.GetStock(ctx, inventorydb.GetStockParams{
		RefID:   uuid.NullUUID{UUID: refID, Valid: true},
		RefType: inventorydb.NullInventoryStockRefType{InventoryStockRefType: refType, Valid: true},
	})
}

// --- Stock ---

type GetStockParams struct {
	RefID   uuid.UUID                         `validate:"required"`
	RefType inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

// GetStock returns stock info for the given reference type and ID.
func (b *InventoryHandler) GetStock(ctx restate.Context, params GetStockParams) (inventorydb.InventoryStock, error) {
	var zero inventorydb.InventoryStock
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate get stock", err)
	}
	return b.getStockByRef(ctx, b.storage.Querier(), params.RefType, params.RefID)
}

type UpdateStockSettingsParams struct {
	RefID          uuid.UUID                         `validate:"required"`
	RefType        inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
	SerialRequired null.Bool                         `validate:"omitnil"`
}

// UpdateStockSettings updates stock settings like serial_required.
func (b *InventoryHandler) UpdateStockSettings(ctx restate.Context, params UpdateStockSettingsParams) (inventorydb.InventoryStock, error) {
	var zero inventorydb.InventoryStock
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate update stock settings", err)
	}

	stock, err := b.getStockByRef(ctx, b.storage.Querier(), params.RefType, params.RefID)
	if err != nil {
		return zero, sharedmodel.WrapErr("db get stock", err)
	}

	return b.storage.Querier().UpdateStock(ctx, inventorydb.UpdateStockParams{
		ID:             stock.ID,
		SerialRequired: params.SerialRequired,
	})
}

type ListStockParams struct {
	sharedmodel.PaginationParams
	RefType []inventorydb.InventoryStockRefType `validate:"dive,required,validateFn=Valid"`
	RefID   []uuid.UUID                         `validate:"dive,required"`
}

// ListStock returns a paginated list of stock records filtered by ref type and ID.
func (b *InventoryHandler) ListStock(ctx restate.Context, params ListStockParams) (sharedmodel.PaginateResult[inventorydb.InventoryStock], error) {
	var zero sharedmodel.PaginateResult[inventorydb.InventoryStock]
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list stock", err)
	}

	rows, err := b.storage.Querier().ListCountStock(ctx, inventorydb.ListCountStockParams{
		Limit:   params.Limit,
		Offset:  params.Offset(),
		RefType: params.RefType,
		RefID:   params.RefID,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list stock", err)
	}

	var total null.Int64
	if len(rows) > 0 {
		total.SetValid(rows[0].TotalCount)
	}

	stocks := lo.Map(rows, func(r inventorydb.ListCountStockRow, _ int) inventorydb.InventoryStock {
		return r.InventoryStock
	})

	return sharedmodel.PaginateResult[inventorydb.InventoryStock]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       stocks,
	}, nil
}

type CreateStockParams struct {
	RefID          uuid.UUID                         `validate:"required"`
	RefType        inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
	Stock          int64                             `validate:"gte=0"`
	SerialRequired bool                              `validate:"omitempty"`
}

// CreateStock creates a new stock record for the given reference.
func (b *InventoryHandler) CreateStock(ctx restate.Context, params CreateStockParams) (inventorydb.InventoryStock, error) {
	var zero inventorydb.InventoryStock
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate create stock", err)
	}

	return b.storage.Querier().CreateDefaultStock(ctx, inventorydb.CreateDefaultStockParams{
		RefType:        params.RefType,
		RefID:          params.RefID,
		Stock:          params.Stock,
		SerialRequired: params.SerialRequired,
	})
}

// --- Stock History ---

type ListStockHistoryParams struct {
	sharedmodel.PaginationParams
	RefID   uuid.UUID                         `validate:"required"`
	RefType inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

// ListStockHistory returns a paginated list of stock change history for the given reference.
func (b *InventoryHandler) ListStockHistory(ctx restate.Context, params ListStockHistoryParams) (sharedmodel.PaginateResult[inventorydb.InventoryStockHistory], error) {
	var zero sharedmodel.PaginateResult[inventorydb.InventoryStockHistory]
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list stock history", err)
	}

	stock, err := b.getStockByRef(ctx, b.storage.Querier(), params.RefType, params.RefID)
	if err != nil {
		return zero, sharedmodel.WrapErr("db get stock", err)
	}

	rows, err := b.storage.Querier().ListCountStockHistory(ctx, inventorydb.ListCountStockHistoryParams{
		StockID: []int64{stock.ID},
		Limit:   params.Limit,
		Offset:  params.Offset(),
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list stock history", err)
	}

	var total null.Int64
	if len(rows) > 0 {
		total.SetValid(rows[0].TotalCount)
	}

	histories := lo.Map(rows, func(r inventorydb.ListCountStockHistoryRow, _ int) inventorydb.InventoryStockHistory {
		return r.InventoryStockHistory
	})

	return sharedmodel.PaginateResult[inventorydb.InventoryStockHistory]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       histories,
	}, nil
}

// --- Import Stock ---

type ImportStockParams struct {
	RefID     uuid.UUID                         `validate:"required"`
	RefType   inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
	Change    int64                             `validate:"required,gt=0"`
	SerialIDs []string                          `validate:"dive,required"`
}

// ImportStock adds stock quantity and optionally creates serial records.
func (b *InventoryHandler) ImportStock(ctx restate.Context, params ImportStockParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate import stock", err)
	}

	q := b.storage.Querier()

	stock, err := b.getStockByRef(ctx, q, params.RefType, params.RefID)
	if err != nil {
		return sharedmodel.WrapErr("db get stock", err)
	}

	if _, err := q.CreateDefaultStockHistory(ctx, inventorydb.CreateDefaultStockHistoryParams{
		StockID: stock.ID,
		Change:  params.Change,
	}); err != nil {
		return sharedmodel.WrapErr("db create stock history", err)
	}

	// Create serials for serialized stock
	if stock.SerialRequired {
		var args []inventorydb.CreateCopyDefaultSerialParams

		if len(params.SerialIDs) != 0 {
			if len(params.SerialIDs) != int(params.Change) {
				return inventorymodel.ErrSerialCountMismatch.Terminal()
			}
			for _, id := range params.SerialIDs {
				args = append(args, inventorydb.CreateCopyDefaultSerialParams{
					ID:      id,
					StockID: stock.ID,
				})
			}
		} else {
			for i := int64(0); i < params.Change; i++ {
				args = append(args, inventorydb.CreateCopyDefaultSerialParams{
					ID:      uuid.NewString(),
					StockID: stock.ID,
				})
			}
		}

		if _, err := q.CreateCopyDefaultSerial(ctx, args); err != nil {
			return sharedmodel.WrapErr("db create serials", err)
		}
	}

	return q.UpdateCurrentStock(ctx, inventorydb.UpdateCurrentStockParams{
		ID:     stock.ID,
		Change: params.Change,
	})
}

// --- Reserve Inventory ---

type ReserveInventoryItem struct {
	RefType inventorydb.InventoryStockRefType
	RefID   uuid.UUID
	Amount  int64
}

type ReserveInventoryResult struct {
	SerialIDs []string
	RefType   inventorydb.InventoryStockRefType
	RefID     uuid.UUID
}

type ReserveInventoryParams struct {
	Items []ReserveInventoryItem
}

// ReserveInventory reserves stock for the given items and assigns serial IDs when required.
func (b *InventoryHandler) ReserveInventory(ctx restate.Context, params ReserveInventoryParams) ([]ReserveInventoryResult, error) {
	var results []ReserveInventoryResult
	q := b.storage.Querier()

	for _, item := range params.Items {
		stock, err := b.getStockByRef(ctx, q, item.RefType, item.RefID)
		if err != nil {
			return nil, err
		}

		if stock.Stock < item.Amount {
			return nil, inventorymodel.ErrOutOfStock.Fmt(item.RefID.String()).Terminal()
		}

		// Adjust inventory and check rows affected
		rowsAffected, err := q.AdjustInventory(ctx, inventorydb.AdjustInventoryParams{
			StockID: stock.ID,
			Amount:  item.Amount,
		})
		if err != nil {
			return nil, err
		}
		if rowsAffected == 0 {
			return nil, inventorymodel.ErrOutOfStock.Fmt(item.RefID.String()).Terminal()
		}

		result := ReserveInventoryResult{
			RefType: item.RefType,
			RefID:   item.RefID,
		}

		// If serial is required, reserve available serials
		if stock.SerialRequired {
			serials, err := q.GetAvailableSerials(ctx, inventorydb.GetAvailableSerialsParams{
				StockID: stock.ID,
				Amount:  int32(item.Amount),
			})
			if err != nil {
				return nil, err
			}

			if len(serials) != int(item.Amount) {
				return nil, inventorymodel.ErrOutOfStock.Fmt(item.RefID.String()).Terminal()
			}

			serialIDs := lo.Map(serials, func(s inventorydb.GetAvailableSerialsRow, _ int) string {
				return s.ID
			})

			if err := q.UpdateSerialStatus(ctx, inventorydb.UpdateSerialStatusParams{
				ID:     serialIDs,
				Status: inventorydb.InventoryStatusTaken,
			}); err != nil {
				return nil, err
			}

			result.SerialIDs = serialIDs
		}

		results = append(results, result)
	}

	return results, nil
}

// --- Serial ---

type UpdateSerialParams struct {
	SerialIDs []string
	Status    inventorydb.InventoryStatus `validate:"required,validateFn=Valid"`
}

// UpdateSerial updates the status of the given serial IDs.
func (b *InventoryHandler) UpdateSerial(ctx restate.Context, params UpdateSerialParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate update serial", err)
	}

	return b.storage.Querier().UpdateSerialStatus(ctx, inventorydb.UpdateSerialStatusParams{
		ID:     params.SerialIDs,
		Status: params.Status,
	})
}

type ListSerialParams struct {
	sharedmodel.PaginationParams
	StockID int64 `validate:"required,gt=0"`
}

// ListSerial returns a paginated list of serials for the given stock ID.
func (b *InventoryHandler) ListSerial(ctx restate.Context, params ListSerialParams) (sharedmodel.PaginateResult[inventorydb.InventorySerial], error) {
	var zero sharedmodel.PaginateResult[inventorydb.InventorySerial]
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list serial", err)
	}

	rows, err := b.storage.Querier().ListCountSerial(ctx, inventorydb.ListCountSerialParams{
		StockID: []int64{params.StockID},
		Limit:   params.Limit,
		Offset:  params.Offset(),
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list serial", err)
	}

	var total null.Int64
	if len(rows) > 0 {
		total.SetValid(rows[0].TotalCount)
	}

	serials := lo.Map(rows, func(r inventorydb.ListCountSerialRow, _ int) inventorydb.InventorySerial {
		return r.InventorySerial
	})

	return sharedmodel.PaginateResult[inventorydb.InventorySerial]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       serials,
	}, nil
}

// --- Most Taken ---

type ListMostTakenSkuParams struct {
	sharedmodel.PaginationParams
	RefType inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

// ListMostTakenSku returns the most reserved SKUs ordered by taken count.
func (b *InventoryHandler) ListMostTakenSku(ctx restate.Context, params ListMostTakenSkuParams) ([]inventorydb.InventoryStock, error) {
	if err := validator.Validate(params); err != nil {
		return nil, sharedmodel.WrapErr("validate list most taken", err)
	}

	return b.storage.Querier().ListMostTakenSku(ctx, inventorydb.ListMostTakenSkuParams{
		Limit:   params.Limit,
		Offset:  params.Offset(),
		RefType: params.RefType,
	})
}
