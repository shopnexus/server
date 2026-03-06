package inventorybiz

import (
	"context"
	"errors"
	"fmt"

	"shopnexus-remastered/internal/infras/pubsub"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	commonmodel "shopnexus-remastered/internal/shared/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type InventoryStorage = pgsqlc.Storage[*inventorydb.Queries]

type InventoryBiz struct {
	storage InventoryStorage
	pubsub  pubsub.Client
}

func NewInventoryBiz(
	storage InventoryStorage,
	pubsub pubsub.Client,
) (*InventoryBiz, error) {
	b := &InventoryBiz{
		storage: storage,
		pubsub:  pubsub.Group("inventory"),
	}

	return b, errors.Join(
		b.InitPubsub(),
	)
}

type ListStockParams struct {
	sharedmodel.PaginationParams
	RefType []inventorydb.InventoryStockRefType `validate:"dive,required,validateFn=Valid"`
	RefID   []uuid.UUID                         `validate:"dive,required"`
}

func (b *InventoryBiz) ListStock(ctx context.Context, params ListStockParams) (sharedmodel.PaginateResult[inventorymodel.Stock], error) {
	var zero sharedmodel.PaginateResult[inventorymodel.Stock]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listStock, err := b.storage.Querier().ListCountStock(ctx, inventorydb.ListCountStockParams{
		Limit:   params.Limit,
		Offset:  params.Offset(),
		RefType: params.RefType,
		RefID:   params.RefID,
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(listStock) > 0 {
		total.SetValid(listStock[0].TotalCount)
	}

	dbStocks := lo.Map(listStock, func(s inventorydb.ListCountStockRow, _ int) inventorydb.InventoryStock {
		return s.InventoryStock
	})

	var stocks []inventorymodel.Stock
	for _, stock := range dbStocks {
		stocks = append(stocks, inventorymodel.Stock{
			ID:          stock.ID,
			RefID:       stock.RefID,
			RefType:     stock.RefType,
			Stock:       stock.Stock,
			Taken:       stock.Taken,
			DateCreated: stock.DateCreated,
		})
	}

	return sharedmodel.PaginateResult[inventorymodel.Stock]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       stocks,
	}, nil
}

type GetStockParams struct {
	RefID   uuid.UUID                         `validate:"required"`
	RefType inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

func (b *InventoryBiz) GetStock(ctx context.Context, params GetStockParams) (inventorymodel.Stock, error) {
	var zero inventorymodel.Stock

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	stock, err := b.storage.Querier().GetStock(ctx, inventorydb.GetStockParams{
		RefID:   uuid.NullUUID{UUID: params.RefID, Valid: true},
		RefType: inventorydb.NullInventoryStockRefType{InventoryStockRefType: params.RefType, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	return inventorymodel.Stock{
		ID:          stock.ID,
		RefID:       stock.RefID,
		RefType:     stock.RefType,
		Stock:       stock.Stock,
		Taken:       stock.Taken,
		DateCreated: stock.DateCreated,
	}, nil
}

type ListStockHistoryParams struct {
	commonmodel.PaginationParams
	RefID   uuid.UUID                         `validate:"required"`
	RefType inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

func (b *InventoryBiz) ListStockHistory(ctx context.Context, params ListStockHistoryParams) (commonmodel.PaginateResult[inventorymodel.StockHistory], error) {
	var zero commonmodel.PaginateResult[inventorymodel.StockHistory]
	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	stock, err := b.storage.Querier().GetStock(ctx, inventorydb.GetStockParams{
		RefID:   uuid.NullUUID{UUID: params.RefID, Valid: true},
		RefType: inventorydb.NullInventoryStockRefType{InventoryStockRefType: params.RefType, Valid: true},
	})
	if err != nil {
		return zero, err
	}

	total, err := b.storage.Querier().CountStockHistory(ctx, inventorydb.CountStockHistoryParams{
		StockID: []int64{stock.ID},
	})
	if err != nil {
		return zero, err
	}

	dbChanges, err := b.storage.Querier().ListStockHistory(ctx, inventorydb.ListStockHistoryParams{
		StockID: []int64{stock.ID},
		Limit:   params.Limit,
		Offset:  params.Offset(),
	})
	if err != nil {
		return zero, err
	}

	var changes []inventorymodel.StockHistory
	for _, change := range dbChanges {
		changes = append(changes, inventorymodel.StockHistory{
			ID:          change.ID,
			Change:      change.Change,
			DateCreated: change.DateCreated,
		})
	}

	return commonmodel.PaginateResult[inventorymodel.StockHistory]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       changes,
	}, nil
}

type CreateStockParams struct {
	Storage InventoryStorage
	Account accountmodel.AuthenticatedAccount
	RefID   uuid.UUID                         `validate:"required"`
	RefType inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
	Stock   int64                             `validate:"required,gt=0"`
}

func (b *InventoryBiz) CreateStock(ctx context.Context, params CreateStockParams) (inventorydb.InventoryStock, error) {
	var zero inventorydb.InventoryStock

	if err := validator.Validate(params); err != nil {
		return zero, err
	}
	var stock inventorydb.InventoryStock

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage InventoryStorage) error {
		var err error
		stock, err = txStorage.Querier().CreateDefaultStock(ctx, inventorydb.CreateDefaultStockParams{
			RefType: params.RefType,
			RefID:   params.RefID,
			Stock:   params.Stock,
		})
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create stock: %w", err)
	}

	return stock, nil
}

type ImportStockParams struct {
	RefID     uuid.UUID                         `validate:"required"`
	RefType   inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
	Change    int64                             `validate:"required,gt=0"`
	SerialIDs []string                          `validate:"dive,required"`
}

func (b *InventoryBiz) ImportStock(ctx context.Context, params ImportStockParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	stock, err := b.storage.Querier().GetStock(ctx, inventorydb.GetStockParams{
		RefID:   uuid.NullUUID{UUID: params.RefID, Valid: true},
		RefType: inventorydb.NullInventoryStockRefType{InventoryStockRefType: params.RefType, Valid: true},
	})
	if err != nil {
		return err
	}

	if _, err := b.storage.Querier().CreateDefaultStockHistory(ctx, inventorydb.CreateDefaultStockHistoryParams{
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

type UpdateSerialParams struct {
	SerialIDs []string
	Status    inventorydb.InventoryStatus `validate:"required,validateFn=Valid"`
}

func (b *InventoryBiz) UpdateSerial(ctx context.Context, params UpdateSerialParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.Querier().UpdateSerialStatus(ctx, inventorydb.UpdateSerialStatusParams{
		ID:     params.SerialIDs,
		Status: params.Status,
	}); err != nil {
		return err
	}

	return nil
}

type ListMostTakenSkuParams struct {
	sharedmodel.PaginationParams
	RefType inventorydb.InventoryStockRefType `validate:"required,validateFn=Valid"`
}

func (b *InventoryBiz) ListMostTakenSku(ctx context.Context, params ListMostTakenSkuParams) ([]inventorydb.InventoryStock, error) {
	var zero []inventorydb.InventoryStock

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	results, err := b.storage.Querier().ListMostTakenSku(ctx, inventorydb.ListMostTakenSkuParams{
		Limit:   params.Limit,
		Offset:  params.Offset(),
		RefType: params.RefType,
	})
	if err != nil {
		return zero, err
	}
	return results, nil
}
