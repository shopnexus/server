package inventorybiz

import (
	"context"
	"fmt"

	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListProductSerialParams struct {
	commonmodel.PaginationParams
	SkuID uuid.UUID `validate:"required"`
}

func (b *InventoryBiz) ListProductSerial(ctx context.Context, params ListProductSerialParams) (commonmodel.PaginateResult[inventorymodel.ProductSerial], error) {
	var zero commonmodel.PaginateResult[inventorymodel.ProductSerial]
	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.Querier().CountSkuSerial(ctx, inventorydb.CountSkuSerialParams{
		SkuID: []uuid.UUID{params.SkuID},
	})
	if err != nil {
		return zero, err
	}

	dbSerials, err := b.storage.Querier().ListSkuSerial(ctx, inventorydb.ListSkuSerialParams{
		SkuID:  []uuid.UUID{params.SkuID},
		Limit:  params.Limit,
		Offset: params.Offset(),
	})
	if err != nil {
		return zero, err
	}

	var serials []inventorymodel.ProductSerial
	for _, serial := range dbSerials {
		serials = append(serials, inventorymodel.ProductSerial{
			ID:          serial.ID,
			SkuID:       serial.SkuID,
			Status:      serial.Status,
			DateCreated: serial.DateCreated,
		})
	}

	return commonmodel.PaginateResult[inventorymodel.ProductSerial]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       serials,
	}, nil
}

type ReserveInventoryParams struct {
	Storage InventoryStorage
	Items   []ReserveIventory
}

type ReserveIventory struct {
	RefType inventorydb.InventoryStockRefType
	RefID   uuid.UUID
	Amount  int64
}

type ReserveInventoryResult struct {
	SerialIDs []string
	RefType   inventorydb.InventoryStockRefType
	RefID     uuid.UUID
}

func (b *InventoryBiz) ReserveInventory(ctx context.Context, params ReserveInventoryParams) ([]ReserveInventoryResult, error) {

	var results []ReserveInventoryResult

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage InventoryStorage) error {
		for _, item := range params.Items {
			var serialIDs []string
			stock, err := txStorage.Querier().GetStock(ctx, inventorydb.GetStockParams{
				RefID:   uuid.NullUUID{UUID: item.RefID, Valid: true},
				RefType: inventorydb.NullInventoryStockRefType{InventoryStockRefType: item.RefType, Valid: true},
			})
			if err != nil {
				return err
			}

			if stock.Stock-stock.Taken < item.Amount {
				return inventorymodel.ErrOutOfStock.Fmt("%s %s", item.RefType, item.RefID)
			}

			// Adjust the inventory (reserve the inventory)
			if err := txStorage.Querier().AdjustInventory(ctx, inventorydb.AdjustInventoryParams{
				RefType: item.RefType,
				RefID:   item.RefID,
				Amount:  item.Amount,
			}); err != nil {
				return err
			}

			// If serial is required, reserve available serials
			if stock.SerialRequired {
				serials, err := txStorage.Querier().GetAvailableSerials(ctx, inventorydb.GetAvailableSerialsParams{
					SkuID:  item.RefID,
					Amount: int32(item.Amount),
				})
				if err != nil {
					return err
				}

				// Check if enough serials are available
				if len(serials) != int(item.Amount) {
					return inventorymodel.ErrOutOfStock.Fmt("%s %s", item.RefType, item.RefID)
				}

				// Reserve the serials
				if err := txStorage.Querier().UpdateSerialStatus(ctx, inventorydb.UpdateSerialStatusParams{
					ID: lo.Map(serials, func(serial inventorydb.GetAvailableSerialsRow, _ int) string {
						return serial.ID
					}),
					Status: inventorydb.InventoryProductStatusSold,
				}); err != nil {
					return err
				}

				serialIDs = lo.Map(serials, func(serial inventorydb.GetAvailableSerialsRow, _ int) string {
					return serial.ID
				})

				results = append(results, ReserveInventoryResult{
					SerialIDs: serialIDs,
					RefType:   item.RefType,
					RefID:     item.RefID,
				})
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("reserve inventory transaction failed: %w", err)
	}

	return results, nil
}
