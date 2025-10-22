package inventorybiz

import (
	"context"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/errutil"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
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

type GetStockParams struct {
	RefID   int64                    `validate:"required,gt=0"`
	RefType db.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

func (b *InventoryBiz) GetStock(ctx context.Context, params GetStockParams) (inventorymodel.Stock, error) {
	var zero inventorymodel.Stock

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	stock, err := b.storage.GetInventoryStock(ctx, db.GetInventoryStockParams{
		RefID:   pgutil.Int64ToPgInt8(params.RefID),
		RefType: db.NullInventoryStockRefType{InventoryStockRefType: params.RefType, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	// dbChanges, err := b.storage.ListInventoryStockHistory(ctx, db.ListInventoryStockHistoryParams{
	// 	StockID: []int64{stock.ID},
	// })
	// if err != nil {
	// 	return zero, err
	// }

	// var changes []inventorymodel.StockHistory
	// for _, change := range dbChanges {
	// 	changes = append(changes, inventorymodel.StockHistory{
	// 		ID:          change.ID,
	// 		Change:      change.Change,
	// 		DateCreated: change.DateCreated.Time,
	// 	})
	// }

	return inventorymodel.Stock{
		ID:           stock.ID,
		RefID:        stock.RefID,
		RefType:      stock.RefType,
		CurrentStock: stock.CurrentStock,
		Sold:         stock.Sold,
		DateCreated:  stock.DateCreated.Time,
		// Changes:      changes,
	}, nil
}

type ListStockHistoryParams struct {
	sharedmodel.PaginationParams
	RefID   int64                    `validate:"required,gt=0"`
	RefType db.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

func (b *InventoryBiz) ListStockHistory(ctx context.Context, params ListStockHistoryParams) (sharedmodel.PaginateResult[inventorymodel.StockHistory], error) {
	var zero sharedmodel.PaginateResult[inventorymodel.StockHistory]
	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	stock, err := b.storage.GetInventoryStock(ctx, db.GetInventoryStockParams{
		RefID:   pgutil.Int64ToPgInt8(params.RefID),
		RefType: db.NullInventoryStockRefType{InventoryStockRefType: params.RefType, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	total, err := b.storage.CountInventoryStockHistory(ctx, db.CountInventoryStockHistoryParams{
		StockID: []int64{stock.ID},
	})
	if err != nil {
		return zero, err
	}

	dbChanges, err := b.storage.ListInventoryStockHistory(ctx, db.ListInventoryStockHistoryParams{
		StockID: []int64{stock.ID},
		Limit:   pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:  pgutil.Int32ToPgInt4(params.Offset()),
	})
	if err != nil {
		return zero, err
	}

	var changes []inventorymodel.StockHistory
	for _, change := range dbChanges {
		changes = append(changes, inventorymodel.StockHistory{
			ID:          change.ID,
			Change:      change.Change,
			DateCreated: change.DateCreated.Time,
		})
	}

	return sharedmodel.PaginateResult[inventorymodel.StockHistory]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       changes,
	}, nil
}

type ImportStockParams struct {
	RefID     int64                    `validate:"required,gt=0"`
	RefType   db.InventoryStockRefType `validate:"required,validateFn=Valid"`
	Change    int64                    `validate:"required,gt=0"`
	SerialIDs []string                 `validate:"dive,required"`
}

func (b *InventoryBiz) ImportStock(ctx context.Context, params ImportStockParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	stock, err := b.storage.GetInventoryStock(ctx, db.GetInventoryStockParams{
		RefID:   pgutil.Int64ToPgInt8(params.RefID),
		RefType: db.NullInventoryStockRefType{InventoryStockRefType: params.RefType, Valid: true},
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
	Status    db.InventoryProductStatus `validate:"required,validateFn=Valid"`
}

func (b *InventoryBiz) UpdateSkuSerial(ctx context.Context, params UpdateSkuSerialParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.UpdateSerialStatus(ctx, db.UpdateSerialStatusParams{
		SerialID: params.SerialIDs,
		Status:   params.Status,
	}); err != nil {
		return err
	}

	return nil
}
