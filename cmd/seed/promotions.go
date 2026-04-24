package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	null "github.com/guregu/null/v6"

	promotiondb "shopnexus-server/internal/module/promotion/db/sqlc"
)

func createPromotionsFromVouchers(
	ctx context.Context,
	vouchers []Voucher,
	spuID uuid.UUID,
	accountID uuid.UUID,
	store *promotiondb.Queries,
) error {
	if len(vouchers) == 0 {
		return nil
	}

	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, voucher := range vouchers {
		var validityEnd, validityStart time.Time
		var err error

		parsed := false
		for _, format := range formats {
			validityEnd, err = time.Parse(format, voucher.ValidityEnd)
			if err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			continue
		}

		parsed = false
		for _, format := range formats {
			validityStart, err = time.Parse(format, voucher.ValidityStart)
			if err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			continue
		}

		if validityEnd.Before(time.Now()) {
			continue
		}

		promotionCode := fmt.Sprintf("PROMO_%s", uuid.New().String())
		if voucher.TextInfo != "" {
			promotionCode = fmt.Sprintf("VOUCHER_%s_%s", strings.ToUpper(voucher.TextInfo), uuid.New().String()[:8])
		}

		minSpend := toBigInt(voucher.MinSpend)
		maxDiscount := toBigInt(voucher.DiscountCap)

		discountData := map[string]any{
			"min_spend":    minSpend,
			"max_discount": maxDiscount,
		}

		if voucher.DiscountPercentage > 0 {
			discountData["discount_percent"] = voucher.DiscountPercentage
		}

		if voucher.Discount != nil && *voucher.Discount > 0 && voucher.DiscountPercentage == 0 {
			discountData["discount_price"] = toBigInt(*voucher.Discount)
		}

		dataJSON, err := json.Marshal(discountData)
		if err != nil {
			return fmt.Errorf("marshal discount data: %w", err)
		}

		promotion, err := store.CreatePromotion(ctx, promotiondb.CreatePromotionParams{
			ID:          uuid.New(),
			Code:        promotionCode,
			OwnerID:     uuid.NullUUID{UUID: accountID, Valid: true},
			Type:        promotiondb.PromotionTypeDiscount,
			Title:       voucher.ShopVoucher,
			Description: null.StringFrom(fmt.Sprintf("Promotion code: %s", voucher.TextInfo)),
			IsEnabled:   true,
			AutoApply:   !voucher.Claimable,
			Group:       "product_discount",
			Data:        dataJSON,
			DateStarted: validityStart,
			DateEnded:   null.TimeFrom(validityEnd),
			DateCreated: time.Now(),
			DateUpdated: time.Now(),
		})
		if err != nil {
			return fmt.Errorf("create promotion: %w", err)
		}

		_, err = store.CreateRef(ctx, promotiondb.CreateRefParams{
			PromotionID: promotion.ID,
			RefID:       spuID,
		})
		if err != nil {
			return fmt.Errorf("create promotion ref: %w", err)
		}
	}

	return nil
}
