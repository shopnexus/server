package inventorybiz

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	"shopnexus-remastered/internal/utils/errutil"
)

func (b *InventoryBiz) InitPubsub() error {
	return errutil.Some(
		b.pubsub.Subscribe(inventorymodel.TopicInventoryStockUpdated, pubsub.DecodeWrap(b.InventoryStockUpdated)),
	)
}

type InventoryStockUpdatedParams struct {
	StockID   int64                    `json:"stock_id"`
	RefType   db.InventoryStockRefType `json:"ref_type"`
	RefID     int64                    `json:"ref_id"`
	Change    int64                    `json:"change"`
	SerialIDs []string                 `json:"serial_ids"`
}

func (b *InventoryBiz) InventoryStockUpdated(ctx context.Context, params InventoryStockUpdatedParams) error {
	var args []db.CreateCopyDefaultInventorySkuSerialParams

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

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

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}
