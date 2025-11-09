package promotionbiz

import (
	"context"
	"fmt"

	"shopnexus-remastered/internal/db"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"

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

	var promotion promotionmodel.PromotionBase
	var discount db.PromotionDiscount

	if err := s.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		params.CreatePromotionParams.Storage = txStorage
		promotion, err = s.createPromotion(ctx, params.CreatePromotionParams)
		if err != nil {
			return err
		}

		discount, err = txStorage.CreateDefaultPromotionDiscount(ctx, db.CreateDefaultPromotionDiscountParams{
			ID:              promotion.ID,
			MinSpend:        params.MinSpend,
			MaxDiscount:     params.MaxDiscount,
			DiscountPercent: pgutil.NullInt32ToPgInt4(params.DiscountPercent),
			DiscountPrice:   pgutil.NullInt64ToPgInt8(params.DiscountPrice),
		})
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
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

	var promotion promotionmodel.PromotionBase
	var discount db.PromotionDiscount

	if err := s.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		// Update base promotion
		params.UpdatePromotionParams.Storage = txStorage
		promotion, err = s.updatePromotion(ctx, params.UpdatePromotionParams)
		if err != nil {
			return err
		}

		// Both percentage and price discount cannot be set
		if params.DiscountPercent.Valid && params.DiscountPrice.Valid {
			return fmt.Errorf("either percentage or price discount can be set, not both")
		}
		var nullDiscountPercent, nullDiscountPrice bool
		if params.DiscountPercent.Valid {
			nullDiscountPrice = true
		}
		if params.DiscountPrice.Valid {
			nullDiscountPercent = true
		}

		discount, err = txStorage.UpdatePromotionDiscount(ctx, db.UpdatePromotionDiscountParams{
			ID:                  promotion.ID,
			MinSpend:            pgutil.NullInt64ToPgInt8(params.MinSpend),
			MaxDiscount:         pgutil.NullInt64ToPgInt8(params.MaxDiscount),
			DiscountPercent:     pgutil.NullInt32ToPgInt4(params.DiscountPercent),
			DiscountPrice:       pgutil.NullInt64ToPgInt8(params.DiscountPrice),
			NullDiscountPercent: nullDiscountPercent,
			NullDiscountPrice:   nullDiscountPrice,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
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
