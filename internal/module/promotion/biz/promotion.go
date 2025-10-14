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

	Code        string              `validate:"required,alphanum,min=3,max=50"`
	RefType     db.PromotionRefType `validate:"required,validateFn=Valid"`
	RefID       null.Int64          `validate:"omitnil"`
	Type        db.PromotionType    `validate:"required,validateFn=Valid"`
	Title       string              `validate:"required,min=3,max=200"`
	Description null.String         `validate:"omitnil,max=1000"`
	IsActive    bool                `validate:"required"`
	DateStarted time.Time           `validate:"required"`
	DateEnded   null.Time           `validate:"omitnil,gtfield=DateStarted"`
}

func (s *PromotionBiz) createPromotion(ctx context.Context, txStorage *pgutil.TxStorage, params CreatePromotionParams) (promotionmodel.PromotionBase, error) {
	var zero promotionmodel.PromotionBase

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	dbPromo, err := txStorage.CreateDefaultPromotionBase(ctx, db.CreateDefaultPromotionBaseParams{
		Code:        params.Code,
		OwnerID:     pgutil.Int64ToPgInt8(params.Account.ID),
		RefType:     params.RefType,
		RefID:       pgutil.NullInt64ToPgInt8(params.RefID),
		Type:        db.PromotionTypeDiscount,
		Title:       params.Title,
		Description: pgutil.NullStringToPgText(params.Description),
		IsActive:    params.IsActive,
		DateStarted: pgutil.TimeToPgTimestamptz(params.DateStarted),
		DateEnded:   pgutil.NullTimeToPgTimestamptz(params.DateEnded),
	})
	if err != nil {
		return zero, err
	}

	return DbPromotionToPromotionBase(dbPromo), nil
}

type UpdatePromotionParams struct {
	ID            int64                           `validate:"required"`
	Code          null.String                     `validate:"omitnil"`
	OwnerID       null.Int64                      `validate:"omitnil"`
	RefType       null.Value[db.PromotionRefType] `validate:"omitnil"`
	RefID         null.Int64                      `validate:"omitnil"`
	Title         null.String                     `validate:"omitnil"`
	Description   null.String                     `validate:"omitnil"`
	IsActive      null.Bool                       `validate:"omitnil"`
	DateStarted   null.Time                       `validate:"omitnil"`
	DateEnded     null.Time                       `validate:"omitnil"`
	NullDateEnded bool                            `validate:"omitempty"`
}

func (s *PromotionBiz) updatePromotion(ctx context.Context, txStorage *pgutil.TxStorage, params UpdatePromotionParams) (promotionmodel.PromotionBase, error) {
	var zero promotionmodel.PromotionBase

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// If RefType is "all", we need to set RefID to null, no need to clear the RefID field as it will be ignored in the update query
	var nullRefID bool
	if params.RefType.Valid && params.RefType.V == db.PromotionRefTypeAll {
		nullRefID = true
	}
	// TODO: check more biz like unique code, valid owner, valid refID for the refType, dateStarted < dateEnded, etc.
	// dateEnded cannot less than dateStarted and current time

	dbPromo, err := txStorage.UpdatePromotionBase(ctx, db.UpdatePromotionBaseParams{
		ID:            params.ID,
		Code:          pgutil.NullStringToPgText(params.Code),
		RefType:       db.NullPromotionRefType{PromotionRefType: params.RefType.V, Valid: params.RefType.Valid},
		NullRefID:     nullRefID,
		RefID:         pgutil.NullInt64ToPgInt8(params.RefID),
		Title:         pgutil.NullStringToPgText(params.Title),
		Description:   pgutil.NullStringToPgText(params.Description),
		IsActive:      pgutil.NullBoolToPgBool(params.IsActive),
		DateStarted:   pgutil.NullTimeToPgTimestamptz(params.DateStarted),
		NullDateEnded: params.NullDateEnded,
		DateUpdated:   pgutil.TimeToPgTimestamptz(time.Now()),
	})
	if err != nil {
		return zero, err
	}

	return DbPromotionToPromotionBase(dbPromo), nil
}

type DeletePromotionParams struct {
	Account authmodel.AuthenticatedAccount
	ID      int64
}

func (s *PromotionBiz) DeletePromotion(ctx context.Context, params DeletePromotionParams) error {
	return s.storage.DeletePromotionBase(ctx, db.DeletePromotionBaseParams{
		ID: []int64{params.ID},
	})
}

func DbPromotionToPromotionBase(dbPromo db.PromotionBase) promotionmodel.PromotionBase {
	return promotionmodel.PromotionBase{
		ID:          dbPromo.ID,
		Code:        dbPromo.Code,
		OwnerID:     pgutil.PgInt8ToNullInt64(dbPromo.OwnerID),
		RefType:     dbPromo.RefType,
		RefID:       pgutil.PgInt8ToNullInt64(dbPromo.RefID),
		Type:        dbPromo.Type,
		Title:       dbPromo.Title,
		Description: pgutil.PgTextToNullString(dbPromo.Description),
		IsActive:    dbPromo.IsActive,
		DateStarted: dbPromo.DateStarted.Time,
		DateEnded:   pgutil.PgTimestamptzToNullTime(dbPromo.DateEnded),
	}
}
