package inventorybiz

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"shopnexus-remastered/internal/infras/pubsub"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	"shopnexus-remastered/internal/shared/validator"
)

func (b *InventoryBiz) InitPubsub() error {
	return errors.Join(
		b.pubsub.Subscribe(inventorymodel.TopicInventoryStockUpdated, pubsub.DecodeWrap(b.InventoryStockUpdated)),
	)
}

type InventoryStockUpdatedParams struct {
	Storage   InventoryStorage
	StockID   int64                             `validate:"required,gt=0"`
	RefType   inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
	RefID     uuid.UUID                         `validate:"required"`
	Change    int64                             `validate:"required,gt=0"`
	SerialIDs []string                          `validate:"omitempty,dive"`
}

func (b *InventoryBiz) InventoryStockUpdated(ctx context.Context, params InventoryStockUpdatedParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	var args []inventorydb.CreateCopyDefaultSkuSerialParams

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage InventoryStorage) error {
		if params.RefType == inventorydb.InventoryStockRefTypeProductSku {
			// Use the vendor passing serial ids (if provided)
			if len(params.SerialIDs) != 0 {
				if len(params.SerialIDs) != int(params.Change) {
					return errors.New("length of serial_ids must be equal to change when serial_ids is provided")
				}

				for _, serialID := range params.SerialIDs {
					args = append(args, inventorydb.CreateCopyDefaultSkuSerialParams{
						ID:    serialID,
						SkuID: params.RefID,
					})
				}
			} else {
				// Use our generated serial ids
				for i := int64(0); i < params.Change; i++ {
					args = append(args, inventorydb.CreateCopyDefaultSkuSerialParams{
						ID:    uuid.NewString(),
						SkuID: params.RefID,
					})
				}
			}

			if _, err := txStorage.Querier().CreateCopyDefaultSkuSerial(ctx, args); err != nil {
				return err
			}
		}

		// Update the current_stock
		if err := txStorage.Querier().UpdateCurrentStock(ctx, inventorydb.UpdateCurrentStockParams{
			ID:     params.StockID,
			Change: params.Change,
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to update inventory stock: %w", err)
	}

	return nil
}
