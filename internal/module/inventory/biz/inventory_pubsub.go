package inventorybiz

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgsqlc"
)

func (b *InventoryBiz) InitPubsub() error {
	return errutil.Some(
		b.pubsub.Subscribe(inventorymodel.TopicInventoryStockUpdated, pubsub.DecodeWrap(b.InventoryStockUpdated)),
	)
}

type InventoryStockUpdatedParams struct {
	Storage   pgsqlc.Storage
	StockID   int64                    `validate:"required,gt=0"`
	RefType   db.InventoryStockRefType `validate:"required,validateFn=Valid"`
	RefID     int64                    `validate:"required,gt=0"`
	Change    int64                    `validate:"required,gt=0"`
	SerialIDs []string                 `validate:"omitempty,dive"`
}

func (b *InventoryBiz) InventoryStockUpdated(ctx context.Context, params InventoryStockUpdatedParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	var args []db.CreateCopyDefaultInventorySkuSerialParams

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		if params.RefType == db.InventoryStockRefTypeProductSku {
			// Use the vendor passing serial ids (if provided)
			if len(params.SerialIDs) != 0 {
				if len(params.SerialIDs) != int(params.Change) {
					return errors.New("length of serial_ids must be equal to change when serial_ids is provided")
				}

				for _, serialID := range params.SerialIDs {
					args = append(args, db.CreateCopyDefaultInventorySkuSerialParams{
						SerialID: serialID,
						SkuID:    params.RefID,
					})
				}
			} else {
				// Use our generated serial ids
				for i := int64(0); i < params.Change; i++ {
					args = append(args, db.CreateCopyDefaultInventorySkuSerialParams{
						SerialID: uuid.NewString(),
						SkuID:    params.RefID,
					})
				}
			}

			if _, err := txStorage.CreateCopyDefaultInventorySkuSerial(ctx, args); err != nil {
				return err
			}
		}

		// Update the current_stock
		if err := txStorage.UpdateCurrentStock(ctx, db.UpdateCurrentStockParams{
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
