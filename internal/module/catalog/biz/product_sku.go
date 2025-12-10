package catalogbiz

import (
	"context"
	"encoding/json"
	"fmt"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/pgutil"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListProductSkuParams struct {
	ID         []uuid.UUID `validate:"omitempty,dive,required"`
	SpuID      []uuid.UUID `validate:"omitempty"`
	PriceFrom  null.Int64  `validate:"omitnil,gt=0"`
	PriceTo    null.Int64  `validate:"omitnil,gt=0,gtefield=PriceFrom"`
	CanCombine null.Bool   `validate:"omitnil"`
}

func (b *CatalogBiz) ListProductSku(ctx context.Context, params ListProductSkuParams) ([]catalogmodel.ProductSku, error) {
	var zero []catalogmodel.ProductSku

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	dbSkus, err := b.storage.Querier().ListProductSku(ctx, catalogdb.ListProductSkuParams{
		SpuID:      params.SpuID,
		PriceFrom:  params.PriceFrom,
		PriceTo:    params.PriceTo,
		CanCombine: pgutil.NullBoolToSlice(params.CanCombine),
	})
	if err != nil {
		return zero, err
	}

	stocks, err := b.inventory.ListStock(ctx, inventorybiz.ListStockParams{
		RefType: []inventorydb.InventoryStockRefType{inventorydb.InventoryStockRefTypeProductSku},
		RefID:   lo.Map(dbSkus, func(s catalogdb.CatalogProductSku, _ int) uuid.UUID { return s.ID }),
	})
	if err != nil {
		return zero, err
	}
	stockMap := lo.KeyBy(stocks.Data, func(s inventorymodel.Stock) uuid.UUID { return s.RefID })

	var skus []catalogmodel.ProductSku
	for _, dbSku := range dbSkus {
		var attributes []catalogmodel.ProductAttribute
		if err := sonic.Unmarshal(dbSku.Attributes, &attributes); err != nil {
			return zero, err
		}
		skus = append(skus, catalogmodel.ProductSku{
			ID:             dbSku.ID,
			SpuID:          dbSku.SpuID,
			Price:          sharedmodel.Concurrency(dbSku.Price),
			CanCombine:     dbSku.CanCombine,
			DateCreated:    dbSku.DateCreated,
			Stock:          stockMap[dbSku.ID].Stock,
			Attributes:     attributes,
			PackageDetails: dbSku.PackageDetails,
		})
	}

	return skus, nil
}

type CreateProductSkuParams struct {
	Storage        CatalogStorage
	Account        accountmodel.AuthenticatedAccount
	SpuID          uuid.UUID                       `validate:"required"`
	Price          sharedmodel.Concurrency         `validate:"required,gt=0"`
	CanCombine     bool                            `validate:"required"`
	Attributes     []catalogmodel.ProductAttribute `validate:"omitempty,dive"`
	PackageDetails json.RawMessage                 `validate:"required"`
}

func (b *CatalogBiz) CreateProductSku(ctx context.Context, params CreateProductSkuParams) (catalogmodel.ProductSku, error) {
	var zero catalogmodel.ProductSku
	var sku catalogdb.CatalogProductSku

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		attributesBytes, err := sonic.Marshal(params.Attributes)
		if err != nil {
			return err
		}
		packagedetailsBytes, err := sonic.Marshal(params.PackageDetails)
		if err != nil {
			return err
		}

		// Create sku
		sku, err = txStorage.Querier().CreateDefaultProductSku(ctx, catalogdb.CreateDefaultProductSkuParams{
			SpuID:          params.SpuID,
			Price:          int64(params.Price),
			CanCombine:     params.CanCombine,
			Attributes:     attributesBytes,
			PackageDetails: packagedetailsBytes,
		})
		if err != nil {
			return err
		}

		// TODO: use message queue
		if _, err := b.inventory.CreateStock(ctx, inventorybiz.CreateStockParams{
			Storage: pgsqlc.NewStorage(txStorage.Conn(), inventorydb.New(txStorage.Conn())),
			Account: params.Account,
			RefID:   sku.ID,
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			Stock:   0,
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create product sku: %w", err)
	}

	return catalogmodel.ProductSku{
		ID:             sku.ID,
		SpuID:          sku.SpuID,
		Price:          sharedmodel.Concurrency(sku.Price),
		CanCombine:     sku.CanCombine,
		DateCreated:    sku.DateCreated,
		Stock:          0,
		Attributes:     params.Attributes,
		PackageDetails: sku.PackageDetails,
	}, nil
}

type UpdateProductSkuParams struct {
	Storage        CatalogStorage
	Account        accountmodel.AuthenticatedAccount
	ID             uuid.UUID                       `validate:"required"`
	Price          sharedmodel.NullConcurrency     `validate:"omitnil"`
	CanCombine     null.Bool                       `validate:"omitnil"`
	Attributes     []catalogmodel.ProductAttribute `validate:"omitnil,dive"`
	PackageDetails json.RawMessage                 `validate:"omitempty"`
}

func (b *CatalogBiz) UpdateProductSku(ctx context.Context, params UpdateProductSkuParams) (catalogmodel.ProductSku, error) {
	var zero catalogmodel.ProductSku

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		sku   catalogdb.CatalogProductSku
		stock inventorymodel.Stock
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		attributesBytes, err := sonic.Marshal(params.Attributes)
		if err != nil {
			return err
		}
		packageDetailsBytes, err := sonic.Marshal(params.PackageDetails)
		if err != nil {
			return err
		}
		// TODO: check biz logic of attribute update

		sku, err = txStorage.Querier().UpdateProductSku(ctx, catalogdb.UpdateProductSkuParams{
			ID:             params.ID,
			Price:          params.Price.ToNullInt64(),
			CanCombine:     params.CanCombine,
			Attributes:     attributesBytes,
			PackageDetails: packageDetailsBytes,
		})
		if err != nil {
			return err
		}

		stock, err = b.inventory.GetStock(ctx, inventorybiz.GetStockParams{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   sku.ID,
		})
		if err != nil {
			return err
		}

		// Invalidate search index for the parent product (spu)
		if err := txStorage.Querier().UpdateStaleSearchSync(ctx, catalogdb.UpdateStaleSearchSyncParams{
			RefType:         catalogmodel.RefTypeProduct,
			RefID:           sku.SpuID,
			IsStaleMetadata: null.BoolFrom(true),
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update product sku: %w", err)
	}

	return catalogmodel.ProductSku{
		ID:             sku.ID,
		SpuID:          sku.SpuID,
		Price:          sharedmodel.Concurrency(sku.Price),
		CanCombine:     sku.CanCombine,
		DateCreated:    sku.DateCreated,
		Stock:          stock.Stock,
		Attributes:     params.Attributes,
		PackageDetails: params.PackageDetails,
	}, nil
}

type DeleteProductSkuParams struct {
	Storage CatalogStorage
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID `validate:"required"`
}

func (b *CatalogBiz) DeleteProductSku(ctx context.Context, params DeleteProductSkuParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage CatalogStorage) error {
		// Delete sku
		if err := txStorage.Querier().DeleteProductSku(ctx, catalogdb.DeleteProductSkuParams{
			ID: []uuid.UUID{params.ID},
		}); err != nil {
			return err
		}

		// TODO: should delete via message queue instead
		// Delete the associated stock record
		// if err := txStorage.Querier().DeleteInventoryStock(ctx, catalogdb.DeleteInventoryStockParams{
		// 	RefType: []catalogdb.InventoryStockRefType{catalogdb.InventoryStockRefTypeProductSku},
		// 	RefID:   []uuid.UUID{params.ID},
		// }); err != nil {
		// 	return err
		// }

		return nil
	}); err != nil {
		return fmt.Errorf("failed to delete product sku: %w", err)
	}

	return nil
}
