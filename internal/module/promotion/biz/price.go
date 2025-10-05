package promotionbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

// CalculatePromotedPrices calculates promoted prices for given SKUs and SPUs
// Returns only the price map: map[skuID]*catalogmodel.ProductPrice
func (s *PromotionBiz) CalculatePromotedPrices(
	ctx context.Context,
	skus []db.CatalogProductSku, // All skus to calculate price for
	spuMap map[int64]*db.CatalogProductSpu, // Map of sku.spuID to SPU
) (map[int64]*catalogmodel.ProductPrice, error) {
	priceMap := make(map[int64]*catalogmodel.ProductPrice)

	// Initialize prices
	for _, sku := range skus {
		priceMap[sku.ID] = &catalogmodel.ProductPrice{
			OriginalPrice: sku.Price,
			Price:         sku.Price,
			SkuID:         sku.ID,
		}
	}

	// Get all active promotions
	promotions, err := s.storage.ListActivePromotion(ctx, db.ListActivePromotionParams{
		AutoApply: pgutil.BoolToPgBool(true), // Only auto-apply promotions
	})
	if err != nil {
		return nil, err
	}
	promotionMap := slice.NewSliceMapID(promotions, func(promo db.PromotionBase) int64 {
		return promo.ID
	})

	promoDiscounts, err := s.storage.ListPromotionDiscount(ctx, db.ListPromotionDiscountParams{
		ID: slice.FilterMap(promotions, func(p db.PromotionBase) (int64, bool) {
			if p.Type == db.PromotionTypeDiscount {
				return p.ID, true
			}
			return 0, false
		}),
	})
	if err != nil {
		return nil, err
	}

	// Apply best discount promotion for each SKU
	for _, sku := range skus {
		price := priceMap[sku.ID]

		for _, discount := range promoDiscounts {
			if !IsPromotionApplicable(*promotionMap.Map[discount.ID], *spuMap[sku.SpuID], price.SkuID) {
				continue
			}

			// Calculate discounted price
			discounted := CalculateDiscountedItemPrice(price.OriginalPrice, discount)
			if price.Price > discounted {
				price.Promotions = append(price.Promotions, *promotionMap.Map[discount.ID])
				price.Price = discounted
			}
		}
	}

	return priceMap, nil
}

func IsPromotionApplicable(promo db.PromotionBase, spu db.CatalogProductSpu, skuID int64) bool {
	if !promo.RefID.Valid {
		return promo.RefType == db.PromotionRefTypeAll
	}

	refID := promo.RefID.Int64
	switch promo.RefType {
	case db.PromotionRefTypeCategory:
		return refID == spu.CategoryID
	case db.PromotionRefTypeBrand:
		return refID == spu.BrandID
	case db.PromotionRefTypeProductSpu:
		return refID == spu.ID
	case db.PromotionRefTypeProductSku:
		return refID == skuID
	case db.PromotionRefTypeAll:
		return true // shouldn't happen since RefID should be null for "all"
	default:
		return false
	}
}

func CalculateDiscountedItemPrice(originalPrice int64, discount db.PromotionDiscount) int64 {
	discountedPrice := originalPrice

	// If the order is apply to specific item and original price is less than the minimum spend, return the original price
	if !discount.OrderWide && originalPrice < discount.MinSpend {
		return originalPrice
	}

	if discount.DiscountPercent.Valid {
		discountAmount := originalPrice * int64(discount.DiscountPercent.Int32) / 100
		discountedPrice -= min(discountAmount, discount.MaxDiscount)
	} else if discount.DiscountPrice.Valid {
		discountedPrice -= min(discount.DiscountPrice.Int64, discount.MaxDiscount)
	}

	if discountedPrice < 0 {
		return 0
	}
	return discountedPrice
}
