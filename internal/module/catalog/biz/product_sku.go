package catalogbiz

import (
	"encoding/json"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	accountmodel "shopnexus-server/internal/module/account/model"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/pgutil"
	"shopnexus-server/internal/shared/validator"

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

// ListProductSku returns product SKUs filtered by ID, SPU, price range, or combinability.
func (b *CatalogHandler) ListProductSku(ctx restate.Context, params ListProductSkuParams) ([]catalogmodel.ProductSku, error) {
	var zero []catalogmodel.ProductSku

	if err := validator.Validate(params); err != nil {
		return zero, restate.TerminalErrorf("validate list product sku: %w", err)
	}

	dbSkus, err := b.storage.Querier().ListProductSku(ctx, catalogdb.ListProductSkuParams{
		ID:         params.ID,
		SpuID:      params.SpuID,
		PriceFrom:  params.PriceFrom,
		PriceTo:    params.PriceTo,
		CanCombine: pgutil.NullBoolToSlice(params.CanCombine),
	})
	if err != nil {
		return zero, fmt.Errorf("list product sku: %w", err)
	}

	stocks, err := b.inventory.ListStock(ctx, inventorybiz.ListStockParams{
		RefType: []inventorydb.InventoryStockRefType{inventorydb.InventoryStockRefTypeProductSku},
		RefID:   lo.Map(dbSkus, func(s catalogdb.CatalogProductSku, _ int) uuid.UUID { return s.ID }),
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list stock", err)
	}
	stockMap := lo.KeyBy(stocks.Data, func(s inventorydb.InventoryStock) uuid.UUID { return s.RefID })

	var skus []catalogmodel.ProductSku
	for _, dbSku := range dbSkus {
		var attributes []catalogmodel.ProductAttribute
		if err := sonic.Unmarshal(dbSku.Attributes, &attributes); err != nil {
			return zero, fmt.Errorf("unmarshal sku attributes: %w", err)
		}
		m := dbToProductSku(dbSku)
		m.Stock = stockMap[dbSku.ID].Stock
		m.Attributes = attributes
		skus = append(skus, m)
	}

	return skus, nil
}

type CreateProductSkuParams struct {
	Account        accountmodel.AuthenticatedAccount
	SpuID          uuid.UUID                       `validate:"required"`
	Price          sharedmodel.Concurrency         `validate:"required,gt=0"`
	CanCombine     bool                            `validate:"required"`
	Attributes     []catalogmodel.ProductAttribute `validate:"omitempty,dive"`
	PackageDetails json.RawMessage                 `validate:"required"`
}

// CreateProductSku creates a new product SKU and initializes its inventory stock.
func (b *CatalogHandler) CreateProductSku(ctx restate.Context, params CreateProductSkuParams) (catalogmodel.ProductSku, error) {
	var zero catalogmodel.ProductSku

	attributesBytes, err := sonic.Marshal(params.Attributes)
	if err != nil {
		return zero, fmt.Errorf("create product sku: %w", err)
	}
	packagedetailsBytes, err := sonic.Marshal(params.PackageDetails)
	if err != nil {
		return zero, fmt.Errorf("create product sku: %w", err)
	}

	// Create sku
	sku, err := b.storage.Querier().CreateDefaultProductSku(ctx, catalogdb.CreateDefaultProductSkuParams{
		SpuID:          params.SpuID,
		Price:          int64(params.Price),
		CanCombine:     params.CanCombine,
		Attributes:     attributesBytes,
		PackageDetails: packagedetailsBytes,
	})
	if err != nil {
		return zero, fmt.Errorf("create product sku: %w", err)
	}

	if _, err := b.inventory.CreateStock(ctx, inventorybiz.CreateStockParams{
		RefID:   sku.ID,
		RefType: inventorydb.InventoryStockRefTypeProductSku,
		Stock:   0,
	}); err != nil {
		return zero, fmt.Errorf("create product sku: %w", err)
	}

	m := dbToProductSku(sku)
	m.Stock = 0
	m.Attributes = params.Attributes
	return m, nil
}

type UpdateProductSkuParams struct {
	Account        accountmodel.AuthenticatedAccount
	ID             uuid.UUID                       `validate:"required"`
	Price          sharedmodel.NullConcurrency     `validate:"omitnil"`
	CanCombine     null.Bool                       `validate:"omitnil"`
	Attributes     []catalogmodel.ProductAttribute `validate:"omitnil,dive"`
	PackageDetails json.RawMessage                 `validate:"omitempty"`
}

// UpdateProductSku updates a product SKU and invalidates the parent SPU search index.
func (b *CatalogHandler) UpdateProductSku(ctx restate.Context, params UpdateProductSkuParams) (catalogmodel.ProductSku, error) {
	var zero catalogmodel.ProductSku

	if err := validator.Validate(params); err != nil {
		return zero, restate.TerminalErrorf("validate update product sku: %w", err)
	}

	attributesBytes, err := sonic.Marshal(params.Attributes)
	if err != nil {
		return zero, fmt.Errorf("update product sku: %w", err)
	}
	packageDetailsBytes, err := sonic.Marshal(params.PackageDetails)
	if err != nil {
		return zero, fmt.Errorf("update product sku: %w", err)
	}
	// TODO: check biz logic of attribute update

	sku, err := b.storage.Querier().UpdateProductSku(ctx, catalogdb.UpdateProductSkuParams{
		ID:             params.ID,
		Price:          params.Price.ToNullInt64(),
		CanCombine:     params.CanCombine,
		Attributes:     attributesBytes,
		PackageDetails: packageDetailsBytes,
	})
	if err != nil {
		return zero, fmt.Errorf("update product sku: %w", err)
	}

	stock, err := b.inventory.GetStock(ctx, inventorybiz.GetStockParams{
		RefType: inventorydb.InventoryStockRefTypeProductSku,
		RefID:   sku.ID,
	})
	if err != nil {
		return zero, fmt.Errorf("update product sku: %w", err)
	}

	// Invalidate search index for the parent product (spu)
	if err := b.storage.Querier().UpdateStaleSearchSync(ctx, catalogdb.UpdateStaleSearchSyncParams{
		RefType:         catalogmodel.RefTypeProduct,
		RefID:           sku.SpuID,
		IsStaleMetadata: null.BoolFrom(true),
	}); err != nil {
		return zero, fmt.Errorf("update product sku: %w", err)
	}

	m := dbToProductSku(sku)
	m.Stock = stock.Stock
	m.Attributes = params.Attributes
	return m, nil
}

// dbToProductSku maps a DB CatalogProductSku row to the model type.
// Callers should set Stock and Attributes as needed.
func dbToProductSku(sku catalogdb.CatalogProductSku) catalogmodel.ProductSku {
	return catalogmodel.ProductSku{
		ID:             sku.ID,
		SpuID:          sku.SpuID,
		Price:          sharedmodel.Concurrency(sku.Price),
		CanCombine:     sku.CanCombine,
		DateCreated:    sku.DateCreated,
		PackageDetails: sku.PackageDetails,
	}
}

type DeleteProductSkuParams struct {
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID `validate:"required"`
}

// DeleteProductSku deletes a product SKU by ID.
func (b *CatalogHandler) DeleteProductSku(ctx restate.Context, params DeleteProductSkuParams) error {
	if err := validator.Validate(params); err != nil {
		return restate.TerminalErrorf("validate delete product sku: %w", err)
	}

	// Delete sku
	if err := b.storage.Querier().DeleteProductSku(ctx, catalogdb.DeleteProductSkuParams{
		ID: []uuid.UUID{params.ID},
	}); err != nil {
		return fmt.Errorf("delete product sku: %w", err)
	}

	// TODO: should delete via message queue instead
	// Delete the associated stock record
	// if err := b.storage.Querier().DeleteInventoryStock(ctx, catalogdb.DeleteInventoryStockParams{
	// 	RefType: []catalogdb.InventoryStockRefType{catalogdb.InventoryStockRefTypeProductSku},
	// 	RefID:   []uuid.UUID{params.ID},
	// }); err != nil {
	// 	return err
	// }

	return nil
}
