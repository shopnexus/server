package promotionbiz

import (
	"context"

	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	promotiondb "shopnexus-remastered/internal/module/promotion/db"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// CalculatePromotedPrices calculates promoted prices for given SKUs and SPUs
// Returns only the price map: map[skuID]*catalogmodel.ProductPrice
func (s *PromotionBiz) CalculatePromotedPrices(
	ctx context.Context,
	prices []catalogmodel.RequestOrderPrice, // Original prices
	spuMap map[uuid.UUID]catalogmodel.ProductSpu, // Map of sku.spuID to SPU
) (map[uuid.UUID]*catalogmodel.OrderPrice, error) {
	var promotionCodes []string
	for _, price := range prices {
		promotionCodes = append(promotionCodes, price.PromotionCodes...)
	}

	// Initialize prices
	priceMap := make(map[uuid.UUID]*catalogmodel.OrderPrice)
	for _, price := range prices {
		priceMap[price.SkuID] = &catalogmodel.OrderPrice{
			Request: price,
		}
	}

	// Get all active promotions
	promotions, err := s.storage.Querier().ListActivePromotion(ctx, promotiondb.ListActivePromotionParams{
		AutoApply: null.BoolFrom(true), // Find auto-apply promotions
		Code:      promotionCodes,      // OR specific promotion codes
	})
	if err != nil {
		return nil, err
	}

	promotionIDs := lo.Map(promotions, func(p promotiondb.PromotionPromotion, _ int) uuid.UUID {
		return p.ID
	})

	refs, err := s.storage.Querier().ListRef(ctx, promotiondb.ListRefParams{
		PromotionID: promotionIDs,
	})
	if err != nil {
		return nil, err
	}
	refsMap := lo.GroupBy(refs, func(r promotiondb.PromotionRef) uuid.UUID {
		return r.PromotionID
	})

	promotionMap := lo.SliceToMap(promotions, func(promo promotiondb.PromotionPromotion) (uuid.UUID, promotionmodel.Promotion) {
		return promo.ID, DbPromotionToPromotionBase(promo, refsMap[promo.ID])
	})

	promoDiscounts, err := s.storage.Querier().ListDiscount(ctx, promotiondb.ListDiscountParams{
		ID: promotionIDs,
	})
	if err != nil {
		return nil, err
	}

	// Apply best discount promotion for each SKU
	for _, price := range prices {
		price := priceMap[price.SkuID]

		bestProductDiscountedPrice := price.Request.UnitPrice.Mul(price.Request.Quantity)

		for _, discount := range promoDiscounts {
			if !IsPromotionApplicable(promotionMap[discount.ID], spuMap[price.Request.SpuID], price.Request.SkuID) {
				continue
			}
			price.PromotionCodes = append(price.PromotionCodes, promotionMap[discount.ID].Code)

			// Calculate product discounted price and take the best price
			productDiscounted := CalculateDiscountedItemPrice(price.Request.UnitPrice.Mul(price.Request.Quantity), discount)
			// TODO: calculate shipping discounted price

			// Take the best discounted price
			if productDiscounted < bestProductDiscountedPrice {
				bestProductDiscountedPrice = productDiscounted
			}
		}

		price.ShipCost = price.Request.ShipCost
		price.ProductCost = bestProductDiscountedPrice
	}

	return priceMap, nil
}

func IsPromotionApplicable(promo promotionmodel.Promotion, spu catalogmodel.ProductSpu, skuID uuid.UUID) bool {
	for _, ref := range promo.Refs {
		refID := ref.RefID
		switch ref.RefType {
		case promotiondb.PromotionRefTypeCategory:
			return spu.Category.ID == refID
		case promotiondb.PromotionRefTypeProductSpu:
			return refID == spu.ID
		case promotiondb.PromotionRefTypeProductSku:
			return refID == skuID
		default:
			return false
		}
	}

	return false
}

// CalculateDiscountedItemPrice calculates the price after applying a discount
func CalculateDiscountedItemPrice(originalPrice sharedmodel.Concurrency, dbDiscount promotiondb.PromotionDiscount) sharedmodel.Concurrency {
	var maxDiscount = sharedmodel.Concurrency(dbDiscount.MaxDiscount)
	var minSpend = sharedmodel.Concurrency(dbDiscount.MinSpend)

	// If original price is less than the minimum spend, return the original price
	if originalPrice < minSpend {
		return originalPrice
	}

	var discount sharedmodel.Concurrency

	if dbDiscount.DiscountPercent.Valid {
		discountAmount := sharedmodel.Concurrency(float64(originalPrice) * dbDiscount.DiscountPercent.Float64 / 100)
		discount = min(discountAmount, maxDiscount)
	} else if dbDiscount.DiscountPrice.Valid {
		discount = min(sharedmodel.Concurrency(dbDiscount.DiscountPrice.Int64), maxDiscount)
	}

	return max(originalPrice-discount, 0)
}

func DbPromotionToPromotionBase(dbPromo promotiondb.PromotionPromotion, refs []promotiondb.PromotionRef) promotionmodel.Promotion {
	return promotionmodel.Promotion{
		ID:          dbPromo.ID,
		Code:        dbPromo.Code,
		Title:       dbPromo.Title,
		Description: dbPromo.Description,
		IsActive:    dbPromo.IsActive,
		AutoApply:   dbPromo.AutoApply,
		DateStarted: dbPromo.DateStarted,
		DateEnded:   dbPromo.DateEnded,
		Refs: lo.Map(refs, func(r promotiondb.PromotionRef, _ int) promotionmodel.PromotionRef {
			return promotionmodel.PromotionRef{
				RefType: r.RefType,
				RefID:   r.RefID,
			}
		}),
		DateCreated: dbPromo.DateCreated,
		DateUpdated: dbPromo.DateUpdated,
	}
}
