package accountbiz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	"shopnexus-remastered/internal/db"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"
)

type GetCartParams struct {
	AccountID int64
}

func (b *AccountBiz) GetCart(ctx context.Context, params GetCartParams) ([]accountmodel.CheckoutSku, error) {
	cartItems, err := b.storage.ListAccountCartItem(ctx, db.ListAccountCartItemParams{
		CartID: []int64{params.AccountID},
	})
	if err != nil {
		return nil, nil
	}
	var orderSkus []OrderSku
	for _, item := range cartItems {
		orderSkus = append(orderSkus, OrderSku{
			SkuID:    item.SkuID,
			Quantity: item.Quantity,
		})
	}

	return b.ListCheckoutSku(ctx, ListCheckoutSkuParams{Skus: orderSkus})
}

type OrderSku struct {
	SkuID    int64 `json:"sku_id"`
	Quantity int64 `json:"quantity"`
}

type ListCheckoutSkuParams struct {
	Skus []OrderSku
}

func (b *AccountBiz) ListCheckoutSku(ctx context.Context, params ListCheckoutSkuParams) ([]accountmodel.CheckoutSku, error) {
	skuIDs := lo.Map(params.Skus, func(c OrderSku, _ int) int64 { return c.SkuID })
	skus, err := b.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{ID: skuIDs})
	if err != nil {
		return nil, nil
	}
	var spuIDs []int64
	skuMap := lo.KeyBy(skus, func(sku db.CatalogProductSku) int64 {
		spuIDs = append(spuIDs, sku.SpuID)
		return sku.ID
	})

	// List all SPUs that user want to see
	spus, err := b.storage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{ID: spuIDs})
	if err != nil {
		return nil, err
	}
	categoryIDs := make([]int64, 0, len(spus))
	spuMap := lo.KeyBy(spus, func(spu db.CatalogProductSpu) int64 {
		categoryIDs = append(categoryIDs, spu.CategoryID)
		return spu.ID
	})

	// Calculate price using promotion biz map[skuID]*catalogmodel.ProductPrice
	priceMap, err := b.promotion.CalculatePromotedPrices(ctx, skus, spuMap)
	if err != nil {
		return nil, err
	}

	// Get first image of the product
	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.CommonResourceRefTypeProductSpu,
		RefID:   spuIDs,
	})
	if err != nil {
		return nil, err
	}
	// map[spuID]resource
	resourcesMap := lo.GroupBy(resources, func(r db.ListSortedResourcesRow) int64 { return r.RefID })

	// Get category
	categories, err := b.storage.ListCatalogCategory(ctx, db.ListCatalogCategoryParams{
		ID: categoryIDs,
	})
	if err != nil {
		return nil, err
	}
	categoryMap := make(map[int64]string) // map[categoryID]name
	for _, cat := range categories {
		categoryMap[cat.ID] = cat.Name
	}

	// Build result
	result := make([]accountmodel.CheckoutSku, 0, len(params.Skus))
	for _, item := range params.Skus {
		sku := skuMap[item.SkuID]
		spu := spuMap[sku.SpuID]
		promos := []int64{}
		for _, promo := range priceMap[sku.ID].Promotions {
			promos = append(promos, promo.ID)
		}

		var attributes []catalogmodel.ProductAttribute
		if err := sonic.Unmarshal(sku.Attributes, &attributes); err != nil {
			return nil, err
		}

		result = append(result, accountmodel.CheckoutSku{
			SkuID:         sku.ID,
			SpuID:         spu.ID,
			Name:          spu.Name,
			SkuName:       GetSkuName(attributes),
			OriginalPrice: priceMap[sku.ID].OriginalPrice,
			Price:         priceMap[sku.ID].Price,
			Quantity:      item.Quantity,
			Resources: lo.Map(resourcesMap[spu.ID], func(r db.ListSortedResourcesRow, _ int) commonmodel.Resource {
				return commonmodel.Resource{
					ID:       r.ID.Bytes,
					Url:      b.common.MustGetFileURL(ctx, r.Provider, r.ObjectKey),
					Mime:     r.Mime,
					Size:     r.Size,
					Checksum: pgutil.PgTextToNullString(r.Checksum),
				}
			}),
			Category:   categoryMap[spu.CategoryID],
			Promotions: promos,
		})
	}

	return result, nil
}

func GetSkuName(attributes []catalogmodel.ProductAttribute) string {
	name := ""
	for _, v := range attributes {
		if name != "" {
			name += ", "
		}
		name += v.Name + ": " + v.Value
	}
	return name
}

type UpdateCartParams struct {
	Account authmodel.AuthenticatedAccount

	SkuID         int64      `validate:"min=1"`
	Quantity      null.Int64 `validate:"omitnil,min=0,max=1000"`
	DeltaQuantity null.Int64 `validate:"omitnil,min=1,max=1000"`
}

func (b *AccountBiz) UpdateCart(ctx context.Context, params UpdateCartParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	var newQuantity int64

	if params.DeltaQuantity.Valid {
		cartItem, err := b.storage.GetAccountCartItem(ctx, db.GetAccountCartItemParams{
			CartID: pgutil.Int64ToPgInt8(params.Account.ID),
			SkuID:  pgutil.Int64ToPgInt8(params.SkuID),
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		newQuantity = cartItem.Quantity + params.DeltaQuantity.Int64
	} else if params.Quantity.Valid {
		newQuantity = params.Quantity.Int64
	} else {
		return fmt.Errorf("either quantity or delta_quantity must be provided")
	}

	// If quantity = 0, remove cart item and return early
	if params.Quantity.Valid && params.Quantity.Int64 <= 0 {
		return b.storage.DeleteAccountCartItem(ctx, db.DeleteAccountCartItemParams{
			CartID: []int64{params.Account.ID},
			SkuID:  []int64{params.SkuID},
		})
	}

	return b.storage.UpdateCart(ctx, db.UpdateCartParams{
		CartID:   params.Account.ID,
		SkuID:    params.SkuID,
		Quantity: newQuantity,
	})
}

type ClearCartParams struct {
	Account authmodel.AuthenticatedAccount
}

func (b *AccountBiz) ClearCart(ctx context.Context, params ClearCartParams) error {
	return b.storage.DeleteAccountCartItem(ctx, db.DeleteAccountCartItemParams{
		CartID: []int64{params.Account.ID},
	})
}
