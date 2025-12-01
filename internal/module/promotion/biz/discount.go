package promotionbiz

import (
	"context"
	"fmt"

	promotiondb "shopnexus-remastered/internal/module/promotion/db/sqlc"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/guregu/null/v6"
)

type CreateDiscountParams struct {
	CreatePromotionParams
	MinSpend        int64      `validate:"required,min=0,max=1000000000"`
	MaxDiscount     int64      `validate:"required,min=0,max=1000000000"`
	DiscountPercent null.Float `validate:"omitnil,min=0,max=1"`
	DiscountPrice   null.Int64 `validate:"omitnil,min=1,max=1000000000"`
}

func (s *PromotionBiz) CreateDiscount(ctx context.Context, params CreateDiscountParams) (promotionmodel.PromotionDiscount, error) {
	var zero promotionmodel.PromotionDiscount

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var promotion promotionmodel.Promotion
	var discount promotiondb.PromotionDiscount

	if err := s.storage.WithTx(ctx, params.Storage, func(txStorage PromotionStorage) error {
		var err error
		params.CreatePromotionParams.Storage = txStorage
		promotion, err = s.createPromotion(ctx, params.CreatePromotionParams)
		if err != nil {
			return err
		}

		discount, err = txStorage.Querier().CreateDefaultDiscount(ctx, promotiondb.CreateDefaultDiscountParams{
			ID:              promotion.ID,
			MinSpend:        params.MinSpend,
			MaxDiscount:     params.MaxDiscount,
			DiscountPercent: params.DiscountPercent,
			DiscountPrice:   params.DiscountPrice,
		})
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return zero, err
	}

	return promotionmodel.PromotionDiscount{
		Promotion:       promotion,
		MinSpend:        sharedmodel.Concurrency(discount.MinSpend),
		MaxDiscount:     sharedmodel.Concurrency(discount.MaxDiscount),
		DiscountPercent: discount.DiscountPercent,
		DiscountPrice:   sharedmodel.NullConcurrencyFromNullInt64(discount.DiscountPrice),
	}, nil
}

type UpdateDiscountParams struct {
	UpdatePromotionParams
	MinSpend        null.Int64 `validate:"omitnil,min=0,max=1000000000"`
	MaxDiscount     null.Int64 `validate:"omitnil,min=0,max=1000000000"`
	DiscountPercent null.Float `validate:"omitnil,min=0,max=1"`
	DiscountPrice   null.Int64 `validate:"omitnil,min=1,max=1000000000"`
}

func (s *PromotionBiz) UpdateDiscount(ctx context.Context, params UpdateDiscountParams) (promotionmodel.PromotionDiscount, error) {
	var zero promotionmodel.PromotionDiscount

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var promotion promotionmodel.Promotion
	var discount promotiondb.PromotionDiscount

	if err := s.storage.WithTx(ctx, params.Storage, func(txStorage PromotionStorage) error {
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

		discount, err = txStorage.Querier().UpdateDiscount(ctx, promotiondb.UpdateDiscountParams{
			ID:                  promotion.ID,
			MinSpend:            params.MinSpend,
			MaxDiscount:         params.MaxDiscount,
			DiscountPercent:     params.DiscountPercent,
			DiscountPrice:       params.DiscountPrice,
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
		Promotion:       promotion,
		MinSpend:        sharedmodel.Concurrency(discount.MinSpend),
		MaxDiscount:     sharedmodel.Concurrency(discount.MaxDiscount),
		DiscountPercent: discount.DiscountPercent,
		DiscountPrice:   sharedmodel.NullConcurrencyFromNullInt64(discount.DiscountPrice),
	}, nil
}
