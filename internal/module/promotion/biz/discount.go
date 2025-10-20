package promotionbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/internal/db"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type CreateDiscountParams struct {
	CreatePromotionParams
	MinSpend        int64      `validate:"required,min=0,max=1000000000"`
	MaxDiscount     int64      `validate:"required,min=0,max=1000000000"`
	DiscountPercent null.Int32 `validate:"omitnil,min=1,max=100"`
	DiscountPrice   null.Int64 `validate:"omitnil,min=1,max=1000000000"`
}

func (s *PromotionBiz) CreateDiscount(ctx context.Context, params CreateDiscountParams) (promotionmodel.PromotionDiscount, error) {
	var zero promotionmodel.PromotionDiscount

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	promotion, err := s.createPromotion(ctx, txStorage, params.CreatePromotionParams)
	if err != nil {
		return zero, err
	}

	discount, err := txStorage.CreateDefaultPromotionDiscount(ctx, db.CreateDefaultPromotionDiscountParams{
		ID:              promotion.ID,
		MinSpend:        params.MinSpend,
		MaxDiscount:     params.MaxDiscount,
		DiscountPercent: pgutil.NullInt32ToPgInt4(params.DiscountPercent),
		DiscountPrice:   pgutil.NullInt64ToPgInt8(params.DiscountPrice),
	})
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return promotionmodel.PromotionDiscount{
		PromotionBase:   promotion,
		MinSpend:        discount.MinSpend,
		MaxDiscount:     discount.MaxDiscount,
		DiscountPercent: pgutil.PgInt4ToNullInt32(discount.DiscountPercent),
		DiscountPrice:   pgutil.PgInt8ToNullInt64(discount.DiscountPrice),
	}, nil
}

type UpdateDiscountParams struct {
	UpdatePromotionParams
	OrderWide       null.Bool  `validate:"omitnil"`
	MinSpend        null.Int64 `validate:"omitnil,min=0,max=1000000000"`
	MaxDiscount     null.Int64 `validate:"omitnil,min=0,max=1000000000"`
	DiscountPercent null.Int32 `validate:"omitnil,min=1,max=100"`
	DiscountPrice   null.Int64 `validate:"omitnil,min=1,max=1000000000"`
}

func (s *PromotionBiz) UpdateDiscount(ctx context.Context, params UpdateDiscountParams) (promotionmodel.PromotionDiscount, error) {
	var zero promotionmodel.PromotionDiscount

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	// Update base promotion
	promotion, err := s.updatePromotion(ctx, txStorage, params.UpdatePromotionParams)
	if err != nil {
		return zero, err
	}

	// Both percentage and price discount cannot be set
	if params.DiscountPercent.Valid && params.DiscountPrice.Valid {
		return zero, fmt.Errorf("either percentage or price discount can be set, not both")
	}
	var nullDiscountPercent, nullDiscountPrice bool
	if params.DiscountPercent.Valid {
		nullDiscountPrice = true
	}
	if params.DiscountPrice.Valid {
		nullDiscountPercent = true
	}

	discount, err := txStorage.UpdatePromotionDiscount(ctx, db.UpdatePromotionDiscountParams{
		ID:                  promotion.ID,
		MinSpend:            pgutil.NullInt64ToPgInt8(params.MinSpend),
		MaxDiscount:         pgutil.NullInt64ToPgInt8(params.MaxDiscount),
		DiscountPercent:     pgutil.NullInt32ToPgInt4(params.DiscountPercent),
		DiscountPrice:       pgutil.NullInt64ToPgInt8(params.DiscountPrice),
		NullDiscountPercent: nullDiscountPercent,
		NullDiscountPrice:   nullDiscountPrice,
	})
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return promotionmodel.PromotionDiscount{
		PromotionBase:   promotion,
		MinSpend:        discount.MinSpend,
		MaxDiscount:     discount.MaxDiscount,
		DiscountPercent: pgutil.PgInt4ToNullInt32(discount.DiscountPercent),
		DiscountPrice:   pgutil.PgInt8ToNullInt64(discount.DiscountPrice),
	}, nil
}
