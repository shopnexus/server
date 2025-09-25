package promotionbiz

import (
	"context"
	"time"

	"github.com/guregu/null/v6"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
)

type PromotionBiz struct {
	storage *pgutil.Storage
}

func NewPromotionBiz(storage *pgutil.Storage) *PromotionBiz {
	return &PromotionBiz{
		storage,
	}
}

type GetPromotionParams struct {
	ID int64
}

func (s *PromotionBiz) GetPromotion(ctx context.Context, params GetPromotionParams) (db.PromotionBase, error) {
	promo, err := s.storage.GetPromotionBase(ctx, db.GetPromotionBaseParams{
		ID: pgutil.PtrToPgtype(&params.ID, pgutil.Int64ToPgInt8),
	})
	if err != nil {
		return db.PromotionBase{}, err
	}

	return promo, nil
}

type ListPromotionParams struct {
	sharedmodel.PaginationParams
}

func (s *PromotionBiz) ListPromotion(ctx context.Context, params ListPromotionParams) (sharedmodel.PaginateResult[db.PromotionBase], error) {
	var zero sharedmodel.PaginateResult[db.PromotionBase]

	total, err := s.storage.CountCatalogProductSku(ctx, db.CountCatalogProductSkuParams{})
	if err != nil {
		return zero, err
	}

	promos, err := s.storage.ListPromotionBase(ctx, db.ListPromotionBaseParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.PromotionBase]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       promos,
	}, nil
}

type CreatePromotionParams struct {
	Account authmodel.AuthenticatedAccount

	Code             string              `validate:"required,alphanum,min=3,max=50"`
	OwnerID          null.Int64          `validate:"omitnil"`
	RefType          db.PromotionRefType `validate:"required,validateFn=Valid"`
	RefID            null.Int64          `validate:"omitnil"`
	Type             db.PromotionType    `validate:"required,validateFn=Valid"`
	Title            string              `validate:"required,min=3,max=200"`
	Description      null.String         `validate:"omitnil,max=1000"`
	IsActive         bool                `validate:"required"`
	DateStarted      time.Time           `validate:"required"`
	DateEnded        null.Time           `validate:"omitnil,gtfield=DateStarted"`
	ScheduleTz       null.String         `validate:"omitnil,timezone"`
	ScheduleStart    null.Time           `validate:"omitnil"`
	ScheduleDuration null.Int32          `validate:"omitnil,gte=0,lte=1440"`
}

type CreateDiscountParams struct {
	CreatePromotionParams
	OrderWide       bool       `validate:"required"`
	MinSpend        int64      `validate:"min=0,max=1000000000"`
	MaxDiscount     int64      `validate:"min=0,max=1000000000"`
	DiscountPercent null.Int32 `validate:"omitnil,min=1,max=100"`
	DiscountPrice   null.Int64 `validate:"omitnil,min=1,max=1000000000"`
}

func (s *PromotionBiz) CreateDiscount(ctx context.Context, params CreateDiscountParams) (promotionmodel.PromotionDiscount, error) {
	var zero promotionmodel.PromotionDiscount

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	promotion, err := s.storage.CreateDefaultPromotionBase(ctx, db.CreateDefaultPromotionBaseParams{
		Code:             params.Code,
		OwnerID:          pgutil.NullInt64ToPgInt8(params.OwnerID),
		RefType:          params.RefType,
		RefID:            pgutil.NullInt64ToPgInt8(params.RefID),
		Type:             db.PromotionTypeDiscount,
		Title:            params.Title,
		Description:      pgutil.NullStringToPgText(params.Description),
		IsActive:         params.IsActive,
		DateStarted:      pgutil.TimeToPgTimestamptz(params.DateStarted),
		DateEnded:        pgutil.NullTimeToPgTimestamptz(params.DateEnded),
		ScheduleTz:       pgutil.NullStringToPgText(params.ScheduleTz),
		ScheduleStart:    pgutil.NullTimeToPgTimestamptz(params.ScheduleStart),
		ScheduleDuration: pgutil.NullInt32ToPgInt4(params.ScheduleDuration),
	})
	if err != nil {
		return zero, err
	}

	discount, err := s.storage.CreateDefaultPromotionDiscount(ctx, db.CreateDefaultPromotionDiscountParams{
		ID:              promotion.ID,
		OrderWide:       params.OrderWide,
		MinSpend:        params.MinSpend,
		MaxDiscount:     params.MaxDiscount,
		DiscountPercent: pgutil.NullInt32ToPgInt4(params.DiscountPercent),
		DiscountPrice:   pgutil.NullInt64ToPgInt8(params.DiscountPrice),
	})
	if err != nil {
		return zero, err
	}

	return promotionmodel.PromotionDiscount{
		PromotionBase: promotionmodel.PromotionBase{
			ID:               discount.ID,
			Code:             promotion.Code,
			OwnerID:          pgutil.PgInt8ToNullInt64(promotion.OwnerID),
			RefType:          promotion.RefType,
			RefID:            pgutil.PgInt8ToNullInt64(promotion.RefID),
			Type:             promotion.Type,
			Title:            promotion.Title,
			Description:      pgutil.PgTextToNullString(promotion.Description),
			IsActive:         promotion.IsActive,
			DateStarted:      promotion.DateStarted.Time,
			DateEnded:        pgutil.PgTimestamptzToNullTime(promotion.DateEnded),
			ScheduleTz:       pgutil.PgTextToNullString(promotion.ScheduleTz),
			ScheduleStart:    pgutil.PgTimestamptzToNullTime(promotion.ScheduleStart),
			ScheduleDuration: pgutil.PgInt4ToNullInt32(promotion.ScheduleDuration),
			DateCreated:      promotion.DateCreated.Time,
			DateUpdated:      promotion.DateUpdated.Time,
		},
	}, nil
}

type UpdatePromotionParams struct {
}

func (s *PromotionBiz) UpdatePromotion(ctx context.Context, params UpdatePromotionParams) error {
	return nil
}

type DeletePromotionParams struct {
	ID int64
}

func (s *PromotionBiz) DeletePromotion(ctx context.Context, params DeletePromotionParams) error {
	return s.storage.DeletePromotionBase(ctx, db.DeletePromotionBaseParams{
		ID: []int64{params.ID},
	})
}
