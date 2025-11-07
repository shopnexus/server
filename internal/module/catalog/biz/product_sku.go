package catalogbiz

import (
	"context"
	"encoding/json"
	"fmt"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	searchmodel "shopnexus-remastered/internal/module/search/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/guregu/null/v6"
)

type ListProductSkuParams struct {
	SpuID      int64      `validate:"omitempty,gt=0"`
	PriceFrom  null.Int64 `validate:"omitnil,gt=0"`
	PriceTo    null.Int64 `validate:"omitnil,gt=0,gtefield=PriceFrom"`
	CanCombine null.Bool  `validate:"omitnil"`
}

func (b *CatalogBiz) ListProductSku(ctx context.Context, params ListProductSkuParams) ([]catalogmodel.ProductSku, error) {
	var zero []catalogmodel.ProductSku

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	dbSkus, err := b.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		SpuID:      []int64{params.SpuID},
		PriceFrom:  pgutil.NullInt64ToPgInt8(params.PriceFrom),
		PriceTo:    pgutil.NullInt64ToPgInt8(params.PriceTo),
		CanCombine: pgutil.NullBoolToSlice(params.CanCombine),
	})
	if err != nil {
		return zero, err
	}

	stocks, err := b.storage.ListInventoryStock(ctx, db.ListInventoryStockParams{
		RefType: []db.InventoryStockRefType{db.InventoryStockRefTypeProductSku},
		RefID:   slice.Map(dbSkus, func(s db.CatalogProductSku) int64 { return s.ID }),
	})
	if err != nil {
		return zero, err
	}
	stockMap := slice.GroupBy(stocks, func(s db.InventoryStock) (int64, db.InventoryStock) { return s.RefID, s })

	var skus []catalogmodel.ProductSku
	for _, dbSku := range dbSkus {
		var attributes []catalogmodel.ProductAttribute
		if err := json.Unmarshal(dbSku.Attributes, &attributes); err != nil {
			return zero, err
		}
		skus = append(skus, catalogmodel.ProductSku{
			ID:          dbSku.ID,
			SpuID:       dbSku.SpuID,
			Price:       dbSku.Price,
			CanCombine:  dbSku.CanCombine,
			DateCreated: dbSku.DateCreated.Time,
			Stock:       stockMap[dbSku.ID].CurrentStock,
			Attributes:  attributes,
		})
	}

	return skus, nil
}

type CreateProductSkuParams struct {
	Storage    pgsqlc.Storage
	Account    authmodel.AuthenticatedAccount
	SpuID      int64                           `validate:"required,gt=0"`
	Price      int64                           `validate:"required,gt=0"`
	CanCombine bool                            `validate:"required"`
	Attributes []catalogmodel.ProductAttribute `validate:"omitempty,dive"`
}

func (b *CatalogBiz) CreateProductSku(ctx context.Context, params CreateProductSkuParams) (catalogmodel.ProductSku, error) {
	var zero catalogmodel.ProductSku
	var sku db.CatalogProductSku

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage *pgsqlc.TxStorage) error {
		attributesBytes, err := json.Marshal(params.Attributes)
		if err != nil {
			return err
		}

		// Create sku
		sku, err = txStorage.CreateDefaultCatalogProductSku(ctx, db.CreateDefaultCatalogProductSkuParams{
			SpuID:      params.SpuID,
			Price:      params.Price,
			CanCombine: params.CanCombine,
			Attributes: attributesBytes,
		})
		if err != nil {
			return err
		}

		// Create sku stock
		if _, err := txStorage.CreateDefaultInventoryStock(ctx, db.CreateDefaultInventoryStockParams{
			RefType: db.InventoryStockRefTypeProductSku,
			RefID:   sku.ID,
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create product sku: %w", err)
	}

	return catalogmodel.ProductSku{
		ID:          sku.ID,
		SpuID:       sku.SpuID,
		Price:       sku.Price,
		CanCombine:  sku.CanCombine,
		DateCreated: sku.DateCreated.Time,
		Stock:       0,
		Attributes:  params.Attributes,
	}, nil
}

type UpdateProductSkuParams struct {
	Storage    pgsqlc.Storage
	Account    authmodel.AuthenticatedAccount
	ID         int64                           `validate:"required,gt=0"`
	Price      null.Int64                      `validate:"omitnil,gt=0"`
	CanCombine null.Bool                       `validate:"omitnil"`
	Attributes []catalogmodel.ProductAttribute `validate:"omitnil,dive"`
}

func (b *CatalogBiz) UpdateProductSku(ctx context.Context, params UpdateProductSkuParams) (catalogmodel.ProductSku, error) {
	var zero catalogmodel.ProductSku

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		sku   db.CatalogProductSku
		stock db.InventoryStock
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage *pgsqlc.TxStorage) error {
		attributesBytes, err := json.Marshal(params.Attributes)
		if err != nil {
			return err
		}
		// TODO: check biz logic of attribute update

		sku, err = txStorage.UpdateCatalogProductSku(ctx, db.UpdateCatalogProductSkuParams{
			ID:         params.ID,
			Price:      pgutil.NullInt64ToPgInt8(params.Price),
			CanCombine: pgutil.NullBoolToPgBool(params.CanCombine),
			Attributes: attributesBytes,
		})
		if err != nil {
			return err
		}

		stock, err = txStorage.GetInventoryStock(ctx, db.GetInventoryStockParams{
			RefType: db.NullInventoryStockRefType{InventoryStockRefType: db.InventoryStockRefTypeProductSku, Valid: true},
			RefID:   pgutil.Int64ToPgInt8(sku.ID),
		})
		if err != nil {
			return err
		}

		// Invalidate search index for the parent product (spu)
		if err := txStorage.UpdateStaleSearchSync(ctx, db.UpdateStaleSearchSyncParams{
			RefType:         searchmodel.RefTypeProduct,
			RefID:           sku.SpuID,
			IsStaleMetadata: pgutil.BoolToPgBool(true),
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update product sku: %w", err)
	}

	return catalogmodel.ProductSku{
		ID:          sku.ID,
		SpuID:       sku.SpuID,
		Price:       sku.Price,
		CanCombine:  sku.CanCombine,
		DateCreated: sku.DateCreated.Time,
		Stock:       stock.CurrentStock,
		Attributes:  params.Attributes,
	}, nil
}

type DeleteProductSkuParams struct {
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount
	ID      int64 `validate:"required,gt=0"`
}

func (b *CatalogBiz) DeleteProductSku(ctx context.Context, params DeleteProductSkuParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage *pgsqlc.TxStorage) error {
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

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete product sku: %w", err)
	}

	return nil
}
