package inventorybiz

import (
	"context"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgutil"
)

type InventoryBiz struct {
	storage *pgutil.Storage
	pubsub  pubsub.Client
}

func NewInventoryBiz(storage *pgutil.Storage, pubsub pubsub.Client) (*InventoryBiz, error) {
	b := &InventoryBiz{
		pubsub:  pubsub.Group("inventory"),
		storage: storage,
	}

	return b, errutil.Some(
		b.InitPubsub(),
	)
}

type AddStockParams struct {
	RefID     int64                 `validate:"required,gt=0"`
	RefType   db.InventoryStockType `validate:"required,validFn=Valid"`
	Change    int64                 `validate:"required,gt=0"`
	SerialIDs []string              `validate:"dive,required"`
}

func (b *InventoryBiz) AddStock(ctx context.Context, params AddStockParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	stock, err := b.storage.GetInventoryStock(ctx, db.GetInventoryStockParams{
		RefID:   pgutil.Int64ToPgInt8(params.RefID),
		RefType: db.NullInventoryStockType{InventoryStockType: params.RefType, Valid: true},
	})
	if err != nil {
		return err
	}

	if _, err := b.storage.CreateDefaultInventoryStockHistory(ctx, db.CreateDefaultInventoryStockHistoryParams{
		StockID: stock.ID,
		Change:  params.Change,
	}); err != nil {
		return err
	}

	// Publish add stock event to pubsub
	if err := b.pubsub.Publish(inventorymodel.TopicInventoryStockUpdated, InventoryStockUpdatedParams{
		StockID:   stock.ID,
		RefType:   stock.RefType,
		RefID:     stock.RefID,
		Change:    params.Change,
		SerialIDs: nil,
	}); err != nil {
		return err
	}

	return nil
}

type UpdateSkuSerialParams struct {
	SerialIDs []string
	Status    db.InventoryProductStatus `validate:"required,validFn=Valid"`
}

func (b *InventoryBiz) UpdateSkuSerial(ctx context.Context, params UpdateSkuSerialParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.UpdateSerialStatus(ctx, db.UpdateSerialStatusParams{
		SerialNumber: params.SerialIDs,
		Status:       params.Status,
	}); err != nil {
		return err
	}

	return nil
}
