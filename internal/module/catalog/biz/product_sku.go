package catalogbiz

import (
	"context"
	"encoding/json"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	searchmodel "shopnexus-remastered/internal/module/search/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListProductSkuParams struct {
	sharedmodel.PaginationParams
	SpuID      []int64 `validate:"omitempty,dive,gt=0"`
	Price      []int64 `validate:"omitempty,dive,gt=0"`
	PriceFrom  null.Int64
	PriceTo    null.Int64
	CanCombine []bool
}

func (b *CatalogBiz) ListProductSku(ctx context.Context, params ListProductSkuParams) (sharedmodel.PaginateResult[db.CatalogProductSku], error) {
	var zero sharedmodel.PaginateResult[db.CatalogProductSku]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountCatalogProductSku(ctx, db.CountCatalogProductSkuParams{
		SpuID:      params.SpuID,
		Price:      params.Price,
		PriceFrom:  pgutil.NullInt64ToPgInt8(params.PriceFrom),
		PriceTo:    pgutil.NullInt64ToPgInt8(params.PriceTo),
		CanCombine: params.CanCombine,
	})
	if err != nil {
		return zero, err
	}

	skus, err := b.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		IDFrom:     params.GetCursorID(),
		Limit:      pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:     pgutil.Int32ToPgInt4(params.Offset()),
		SpuID:      params.SpuID,
		Price:      params.Price,
		PriceFrom:  pgutil.NullInt64ToPgInt8(params.PriceFrom),
		PriceTo:    pgutil.NullInt64ToPgInt8(params.PriceTo),
		CanCombine: params.CanCombine,
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.CatalogProductSku]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       skus,
	}, nil
}

type CreateProductSkuParams struct {
	Account    authmodel.AuthenticatedAccount
	SpuID      int64             `validate:"required,gt=0"`
	Price      int64             `validate:"required,gt=0"`
	CanCombine bool              `validate:"required"`
	Attribute  map[string]string `validate:"omitempty,dive,keys,min=1,max=100,endkeys,min=1,max=100"`
}

func (b *CatalogBiz) CreateProductSku(ctx context.Context, params CreateProductSkuParams) (db.CatalogProductSku, error) {
	var zero db.CatalogProductSku
	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	attributesBytes, err := json.Marshal(params.Attribute)
	if err != nil {
		return zero, err
	}

	// Create sku
	sku, err := txStorage.CreateDefaultCatalogProductSku(ctx, db.CreateDefaultCatalogProductSkuParams{
		SpuID:      params.SpuID,
		Price:      params.Price,
		CanCombine: params.CanCombine,
		Attributes: attributesBytes,
	})
	if err != nil {
		return zero, err
	}

	// Create sku stock
	if _, err := txStorage.CreateDefaultInventoryStock(ctx, db.CreateDefaultInventoryStockParams{
		RefType: db.InventoryStockRefTypeProductSku,
		RefID:   sku.ID,
	}); err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return sku, nil
}

type UpdateProductSkuParams struct {
	Account    authmodel.AuthenticatedAccount
	ID         int64             `validate:"required,gt=0"`
	Price      null.Int64        `validate:"omitnil,gt=0"`
	CanCombine null.Bool         `validate:"omitnil"`
	Attribute  map[string]string `validate:"omitnil,dive,keys,min=1,max=100,endkeys,min=1,max=100"`
}

func (b *CatalogBiz) UpdateProductSku(ctx context.Context, params UpdateProductSkuParams) (db.CatalogProductSku, error) {
	var zero db.CatalogProductSku

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	attributesBytes, err := json.Marshal(params.Attribute)
	if err != nil {
		return zero, err
	}
	// TODO: check biz logic of attribute update

	sku, err := txStorage.UpdateCatalogProductSku(ctx, db.UpdateCatalogProductSkuParams{
		ID:         params.ID,
		Price:      pgutil.NullInt64ToPgInt8(params.Price),
		CanCombine: pgutil.NullBoolToPgBool(params.CanCombine),
		Attributes: attributesBytes,
	})
	if err != nil {
		return zero, err
	}

	// Invalidate search index for the parent product (spu)
	if err := txStorage.UpdateStaleSearchSync(ctx, db.UpdateStaleSearchSyncParams{
		RefType:         searchmodel.RefTypeProduct,
		RefID:           sku.SpuID,
		IsStaleMetadata: pgutil.BoolToPgBool(true),
	}); err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return sku, nil
}

type DeleteProductSkuParams struct {
	Account authmodel.AuthenticatedAccount
	ID      int64 `validate:"required,gt=0"`
}

func (b *CatalogBiz) DeleteProductSku(ctx context.Context, params DeleteProductSkuParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	// Delete sku
	if err := txStorage.DeleteCatalogProductSku(ctx, db.DeleteCatalogProductSkuParams{
		ID: []int64{params.ID},
	}); err != nil {
		return err
	}

	// Delete the associated stock record
	if err := txStorage.DeleteInventoryStock(ctx, db.DeleteInventoryStockParams{
		RefType: []db.InventoryStockRefType{db.InventoryStockRefTypeProductSku},
		RefID:   []int64{params.ID},
	}); err != nil {
		return err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}
