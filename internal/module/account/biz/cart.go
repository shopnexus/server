package accountbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	accountmodel "shopnexus-remastered/internal/module/account/model"
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
	priceMap, err := s.promotionBiz.CalculatePromotedPrices(ctx, skus, spuMap.Map)
	if err != nil {
		return nil, err
	}

	// Get first image of the product
	resources, err := s.storage.ListSharedResourceFirst(ctx, db.ListSharedResourceFirstParams{
		OwnerType: db.SharedResourceTypeProductSpu,
		OwnerID:   spuIDs,
	})
	resourceMap := slice.NewMap(resources, func(res db.ListSharedResourceFirstRow) int64 { return res.OwnerID }) // map[ownerID]url

	// Get attributes
	attributes, err := s.storage.ListCatalogProductSkuAttribute(ctx, db.ListCatalogProductSkuAttributeParams{
		SkuID: skuIDs,
	})
	if err != nil {
		return nil, err
	}
	attributeMap := make(map[int64][]db.CatalogProductSkuAttribute)
	for _, attr := range attributes {
		attributeMap[attr.SkuID] = append(attributeMap[attr.SkuID], attr)
	}

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

		result = append(result, accountmodel.CartItem{
			SkuID:         sku.ID,
			Name:          spu.Name,
			SkuName:       GetSkuName(attributeMap[sku.ID]),
			OriginalPrice: priceMap[sku.ID].OriginalPrice,
			Price:         priceMap[sku.ID].Price,
			Quantity:      item.Quantity,
			Image:         resourceMap[spu.ID].Url,
			Category:      categoryMap[spu.CategoryID],
			Promotions:    promos,
			BulkPrice:     nil, // TODO
			BulkThreshold: nil,
		})
	}

	return result, nil
}

func GetSkuName(attributes []db.CatalogProductSkuAttribute) string {
	name := ""
	for i, attr := range attributes {
		if i > 0 {
			name += ", "
		}
		name += attr.Name + " " + attr.Value
	}
	return name
}
