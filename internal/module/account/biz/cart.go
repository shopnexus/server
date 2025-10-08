package accountbiz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/guregu/null/v6"

	"shopnexus-remastered/internal/db"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

type GetCartParams struct {
	AccountID int64
}

func (s *AccountBiz) GetCart(ctx context.Context, params GetCartParams) ([]accountmodel.CartItem, error) {
	cartItems, err := s.storage.ListAccountCartItem(ctx, db.ListAccountCartItemParams{
		CartID: []int64{params.AccountID},
	})
	if err != nil {
		return nil, nil
	}
	skuIDs := make([]int64, 0, len(cartItems))
	for _, item := range cartItems {
		skuIDs = append(skuIDs, item.SkuID)
	}

	skus, err := s.storage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{ID: skuIDs})
	if err != nil {
		return nil, nil
	}
	var spuIDs []int64
	skuMap := slice.NewSliceMapID(skus, func(sku db.CatalogProductSku) int64 {
		spuIDs = append(spuIDs, sku.SpuID)
		return sku.ID
	})

	// List all SPUs that user want to see
	spus, err := s.storage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{ID: spuIDs})
	if err != nil {
		return nil, err
	}
	categoryIDs := make([]int64, 0, len(spus))
	spuMap := slice.NewSliceMapID(spus, func(spu db.CatalogProductSpu) int64 {
		categoryIDs = append(categoryIDs, spu.CategoryID)
		return spu.ID
	})

	// Calculate price using promotion biz map[skuID]*catalogmodel.ProductPrice
	priceMap, err := s.promotion.CalculatePromotedPrices(ctx, skus, spuMap.Map)
	if err != nil {
		return nil, err
	}

	// Get first image of the product
	resources, err := s.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType:   db.SharedResourceRefTypeProductSpu,
		RefID:     spuIDs,
		IsPrimary: pgutil.BoolToPgBool(true),
	})
	if err != nil {
		return nil, err
	}
	// map[spuID]resource
	resourceMap := slice.NewMap(resources, func(r db.ListSortedResourcesRow) int64 { return r.RefID })

	// Get category
	categories, err := s.storage.ListCatalogCategory(ctx, db.ListCatalogCategoryParams{
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
	result := make([]accountmodel.CartItem, 0, len(cartItems))
	for _, item := range cartItems {
		sku := skuMap.Map[item.SkuID]
		spu := spuMap.Map[sku.SpuID]
		promos := []int64{}
		for _, promo := range priceMap[sku.ID].Promotions {
			promos = append(promos, promo.ID)
		}

		var attributes []catalogmodel.ProductAttribute
		if err := json.Unmarshal(sku.Attributes, &attributes); err != nil {
			return nil, err
		}

		result = append(result, accountmodel.CartItem{
			SkuID:         sku.ID,
			SpuID:         spu.ID,
			Name:          spu.Name,
			SkuName:       GetSkuName(attributes),
			OriginalPrice: priceMap[sku.ID].OriginalPrice,
			Price:         priceMap[sku.ID].Price,
			Quantity:      item.Quantity,
			Resource: sharedmodel.Resource{
				ID:       resourceMap[spu.ID].ID,
				Url:      sharedbiz.GetResourceURL(resourceMap[spu.ID].Code),
				Mime:     resourceMap[spu.ID].Mime,
				FileSize: pgutil.PgInt8ToNullInt64(resourceMap[spu.ID].FileSize),
				Width:    pgutil.PgInt4ToNullInt32(resourceMap[spu.ID].Width),
				Height:   pgutil.PgInt4ToNullInt32(resourceMap[spu.ID].Height),
				Duration: pgutil.PgFloat8ToNullFloat(resourceMap[spu.ID].Duration),
			},
			Category:      categoryMap[spu.CategoryID],
			Promotions:    promos,
			BulkPrice:     nil, // TODO
			BulkThreshold: nil,
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

func (s *AccountBiz) UpdateCart(ctx context.Context, params UpdateCartParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	var newQuantity int64

	if params.DeltaQuantity.Valid {
		cartItem, err := s.storage.GetAccountCartItem(ctx, db.GetAccountCartItemParams{
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
		return s.storage.DeleteAccountCartItem(ctx, db.DeleteAccountCartItemParams{
			CartID: []int64{params.Account.ID},
			SkuID:  []int64{params.SkuID},
		})
	}

	return s.storage.UpdateCart(ctx, db.UpdateCartParams{
		CartID:   params.Account.ID,
		SkuID:    params.SkuID,
		Quantity: newQuantity,
	})
}

type ClearCartParams struct {
	Account authmodel.AuthenticatedAccount
}

func (s *AccountBiz) ClearCart(ctx context.Context, params ClearCartParams) error {
	return s.storage.DeleteAccountCartItem(ctx, db.DeleteAccountCartItemParams{
		CartID: []int64{params.Account.ID},
	})
}
