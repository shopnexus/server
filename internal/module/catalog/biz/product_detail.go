package catalogbiz

import (
	"context"
	"database/sql"
	"errors"

	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commondb "shopnexus-remastered/internal/module/common/db/sqlc"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	inventorymodel "shopnexus-remastered/internal/module/inventory/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

func (b *CatalogBiz) GetProductDetail(ctx context.Context, id uuid.UUID) (catalogmodel.ProductDetail, error) {
	var zero catalogmodel.ProductDetail

	spu, err := b.GetProductSpu(ctx, id)
	if err != nil {
		return zero, err
	}

	var skusDetail []catalogmodel.ProductDetailSku
	skus, err := b.ListProductSku(ctx, ListProductSkuParams{
		SpuID: []uuid.UUID{spu.ID},
	})
	if err != nil {
		return zero, err
	}
	skuIDs := lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID { return s.ID })

	// Get sold count from inventory
	listStock, err := b.inventory.ListStock(ctx, inventorybiz.ListStockParams{
		RefType: []inventorydb.InventoryStockRefType{inventorydb.InventoryStockRefTypeProductSku},
		RefID:   skuIDs,
	})
	if err != nil {
		return zero, err
	}
	stockMap := lo.KeyBy(listStock.Data, func(s inventorymodel.Stock) uuid.UUID { return s.RefID })

	// Calculate promoted prices for SKUs
	requestPrices := make([]catalogmodel.RequestOrderPrice, 0, len(skus))
	for _, sku := range skus {
		requestPrices = append(requestPrices, catalogmodel.RequestOrderPrice{
			SkuID:     sku.ID,
			SpuID:     sku.SpuID,
			UnitPrice: sharedmodel.Concurrency(sku.Price),
			Quantity:  1,
			ShipCost:  0,
		})
	}

	priceMap, err := b.promotion.CalculatePromotedPrices(ctx, requestPrices, map[uuid.UUID]catalogmodel.ProductSpu{
		spu.ID: spu,
	})
	if err != nil {
		return zero, err
	}

	for _, sku := range skus {
		priceValue := sharedmodel.Concurrency(sku.Price)
		originalPrice := sharedmodel.Concurrency(sku.Price)
		if priceInfo, ok := priceMap[sku.ID]; ok && priceInfo != nil {
			originalPrice = priceInfo.Request.UnitPrice
			if priceInfo.ProductCost != 0 {
				priceValue = priceInfo.ProductCost
			}
		}

		var taken int64
		if stock, ok := stockMap[sku.ID]; ok {
			taken = stock.Taken
		}

		skusDetail = append(skusDetail, catalogmodel.ProductDetailSku{
			ID:            sku.ID,
			Price:         priceValue,
			OriginalPrice: originalPrice,
			Attributes:    sku.Attributes,
			Taken:         taken,
		})
	}

	// Get images
	resourcesMap, err := b.common.GetResources(ctx, commondb.CommonResourceRefTypeProductSpu, []uuid.UUID{spu.ID})

	// get rating
	rating, err := b.storage.Querier().DetailRating(ctx, catalogdb.DetailRatingParams{
		RefType: catalogdb.CatalogCommentRefTypeProductSpu,
		RefID:   spu.ID,
	})
	ratingBreakdown := make(map[int]int)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	ratingBreakdown[5] = int(rating.FiveCount)
	ratingBreakdown[4] = int(rating.FourCount)
	ratingBreakdown[3] = int(rating.ThreeCount)
	ratingBreakdown[2] = int(rating.TwoCount)
	ratingBreakdown[1] = int(rating.OneCount)

	promoSet := make(map[string]struct{})
	var promotions []catalogmodel.ProductCardPromo
	for _, price := range priceMap {
		if price == nil {
			continue
		}
		for _, code := range price.PromotionCodes {
			if _, exists := promoSet[code]; exists {
				continue
			}
			promoSet[code] = struct{}{}
			promotions = append(promotions, catalogmodel.ProductCardPromo{
				Title: code,
			})
		}
	}

	return catalogmodel.ProductDetail{
		ID:          spu.ID,
		Slug:        spu.Slug,
		VendorID:    spu.AccountID,
		Name:        spu.Name,
		Description: spu.Description,
		Brand:       spu.Brand,
		IsActive:    spu.IsActive,
		Category:    spu.Category,
		Rating: catalogmodel.ProductRating{
			Score:     rating.Score,
			Total:     rating.Count,
			Breakdown: ratingBreakdown,
		},
		Resources:      resourcesMap[spu.ID],
		Promotions:     promotions,
		Skus:           skusDetail,
		Specifications: spu.Specifications,
	}, nil
}
