package promotionbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

// CalculatePromotedPrices calculates promoted prices for given SKUs and SPUs
// Returns only the price map: map[skuID]*catalogmodel.ProductPrice
func (s *PromotionBiz) CalculatePromotedPrices(
	ctx context.Context,
	skus []db.CatalogProductSku, // All skus to calculate price for
	spuMap map[int64]db.CatalogProductSpu, // Map of sku.spuID to SPU
) (map[int64]*catalogmodel.ProductPrice, error) {
	priceMap := make(map[int64]*catalogmodel.ProductPrice)

	// Initialize prices
	for _, sku := range skus {
		priceMap[sku.ID] = &catalogmodel.ProductPrice{
			OriginalPrice: sharedmodel.Int64ToConcurrency(sku.Price),
			Price:         sharedmodel.Int64ToConcurrency(sku.Price),
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

	refs, err := s.storage.ListPromotionRef(ctx, db.ListPromotionRefParams{
		PromotionID: slice.Map(promotions, func(p db.PromotionBase) int64 {
			return p.ID
		}),
	})
	if err != nil {
		return nil, err
	}
	refsMap := slice.GroupBySlice(refs, func(r db.PromotionRef) (int64, db.PromotionRef) {
		return r.PromotionID, r
	})

	promotionMap := slice.GroupBy(promotions, func(promo db.PromotionBase) (int64, promotionmodel.PromotionBase) {
		return promo.ID, DbPromotionToPromotionBase(promo, refsMap[promo.ID])
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
			if !IsPromotionApplicable(promotionMap[discount.ID], spuMap[sku.SpuID], price.SkuID) {
				continue
			}

			// Calculate discounted price
			discounted := CalculateDiscountedItemPrice(price.OriginalPrice, discount)
			if price.Price > discounted {
				price.Promotions = append(price.Promotions, promotionMap[discount.ID])
				price.Price = discounted
			}
		}
	}

	return priceMap, nil
}

func IsPromotionApplicable(promo promotionmodel.PromotionBase, spu db.CatalogProductSpu, skuID int64) bool {
	for _, ref := range promo.Refs {
		refID := ref.RefID
		switch ref.RefType {
		case db.PromotionRefTypeCategory:
			return refID == spu.CategoryID
		case db.PromotionRefTypeBrand:
			return refID == spu.BrandID
		case db.PromotionRefTypeProductSpu:
			return refID == spu.ID
		case db.PromotionRefTypeProductSku:
			return refID == skuID
		default:
			return false
		}
	}

	return false
}

func CalculateDiscountedItemPrice(originalPrice sharedmodel.Concurrency, dbDiscount db.PromotionDiscount) sharedmodel.Concurrency {
	// If original price is less than the minimum spend, return the original price
	if originalPrice.Int64() < dbDiscount.MinSpend {
		return originalPrice
	}

	var discount int64
	if dbDiscount.DiscountPercent.Valid {
		discountAmount := originalPrice.Int64() * int64(dbDiscount.DiscountPercent.Int32) / 100
		discount = min(discountAmount, dbDiscount.MaxDiscount)
	} else if dbDiscount.DiscountPrice.Valid {
		discount = min(dbDiscount.DiscountPrice.Int64, dbDiscount.MaxDiscount)
	}

	discountedPrice := sharedmodel.Int64ToConcurrency(originalPrice.Int64() - discount)

	if discountedPrice.Int64() < 0 {
		return 0
	}
	return discountedPrice
}
