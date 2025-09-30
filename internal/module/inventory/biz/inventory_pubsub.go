package inventorybiz

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgutil"
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

	// Update the current_stock
	txStorage.UpdateInventoryStock(ctx, db.UpdateInventoryStockParams{
		RefType:      db.NullInventoryStockRefType{InventoryStockRefType: params.RefType, Valid: true},
		RefID:        pgutil.Int64ToPgInt8(params.RefID),
		CurrentStock: pgutil.Int64ToPgInt8(params.Change),
		Sold:         pgtype.Int8{},
		DateCreated:  pgtype.Timestamptz{},
		ID:           0,
	})

	if params.RefType == db.InventoryStockRefTypeProductSku {
		// Use the vendor passing serial ids (if provided)
		if len(params.SerialIDs) != 0 {
			if len(params.SerialIDs) != int(params.Change) {
				return errors.New("length of serial_ids must be equal to change when serial_ids is provided")
			}

			for _, serialID := range params.SerialIDs {
				args = append(args, db.CreateCopyDefaultInventorySkuSerialParams{
					SerialNumber: serialID,
					SkuID:        params.RefID,
				})
			}
		} else {
			// Use our generated serial ids
			for i := int64(0); i < params.Change; i++ {
				args = append(args, db.CreateCopyDefaultInventorySkuSerialParams{
					SerialNumber: uuid.NewString(),
					SkuID:        0,
				})
			}
		}

		if _, err := txStorage.CreateCopyDefaultInventorySkuSerial(ctx, args); err != nil {
			return err
		}
	}

	return nil
}
