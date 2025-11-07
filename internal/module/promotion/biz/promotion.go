package promotionbiz

import (
	"context"
	"fmt"
	"time"

	"github.com/guregu/null/v6"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"
)

type PromotionBiz struct {
	storage pgsqlc.Storage
}

func NewPromotionBiz(storage pgsqlc.Storage) *PromotionBiz {
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
	commonmodel.PaginationParams
}

func (s *PromotionBiz) ListPromotion(ctx context.Context, params ListPromotionParams) (commonmodel.PaginateResult[promotionmodel.PromotionBase], error) {
	var zero commonmodel.PaginateResult[promotionmodel.PromotionBase]

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

	refs, err := s.storage.ListPromotionRef(ctx, db.ListPromotionRefParams{
		PromotionID: slice.Map(promos, func(p db.PromotionBase) int64 {
			return p.ID
		}),
	})
	if err != nil {
		return zero, err
	}
	refsMap := slice.GroupBySlice(refs, func(r db.PromotionRef) (int64, db.PromotionRef) {
		return r.PromotionID, r
	})

	return commonmodel.PaginateResult[promotionmodel.PromotionBase]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data: slice.Map(promos, func(p db.PromotionBase) promotionmodel.PromotionBase {
			return DbPromotionToPromotionBase(p, refsMap[p.ID])
		}),
	}, nil
}

type CreatePromotionParams struct {
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount

	Code        string           `validate:"required,alphanum,min=3,max=50"`
	Refs        []PromotionRef   `validate:"dive"`
	Type        db.PromotionType `validate:"required,validateFn=Valid"`
	Title       string           `validate:"required,min=3,max=200"`
	Description null.String      `validate:"omitnil,max=1000"`
	IsActive    bool             `validate:"omitempty"`
	AutoApply   bool             `validate:"omitempty"`
	DateStarted time.Time        `validate:"required"`
	DateEnded   null.Time        `validate:"omitnil,gtfield=DateStarted"`
}

type PromotionRef struct {
	RefType db.PromotionRefType `validate:"required,validateFn=Valid"`
	RefID   int64               `validate:"required"`
}

func (b *PromotionBiz) createPromotion(ctx context.Context, params CreatePromotionParams) (promotionmodel.PromotionBase, error) {
	var zero promotionmodel.PromotionBase

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var dbPromo db.PromotionBase

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error

		dbPromo, err := txStorage.CreateDefaultPromotionBase(ctx, db.CreateDefaultPromotionBaseParams{
			Code:        params.Code,
			OwnerID:     pgutil.Int64ToPgInt8(params.Account.ID),
			Type:        db.PromotionTypeDiscount,
			Title:       params.Title,
			Description: pgutil.NullStringToPgText(params.Description),
			IsActive:    params.IsActive,
			AutoApply:   params.AutoApply,
			DateStarted: pgutil.TimeToPgTimestamptz(params.DateStarted),
			DateEnded:   pgutil.NullTimeToPgTimestamptz(params.DateEnded),
		})
		if err != nil {
			return err
		}

		_, err = txStorage.CreateCopyDefaultPromotionRef(ctx, slice.Map(params.Refs, func(r PromotionRef) db.CreateCopyDefaultPromotionRefParams {
			return db.CreateCopyDefaultPromotionRefParams{
				PromotionID: dbPromo.ID,
				RefType:     r.RefType,
				RefID:       r.RefID,
			}
		}))
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create promotion: %w", err)
	}

	return DbPromotionToPromotionBase(dbPromo, nil), nil
}

type UpdatePromotionParams struct {
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount

	ID            int64          `validate:"required"`
	Code          null.String    `validate:"omitnil"`
	OwnerID       null.Int64     `validate:"omitnil"`
	Title         null.String    `validate:"omitnil"`
	Description   null.String    `validate:"omitnil"`
	IsActive      null.Bool      `validate:"omitnil"`
	AutoApply     null.Bool      `validate:"omitnil"`
	DateStarted   null.Time      `validate:"omitnil"`
	DateEnded     null.Time      `validate:"omitnil"`
	NullDateEnded bool           `validate:"omitempty"`
	Refs          []PromotionRef `validate:"dive"`
}

func (s *PromotionBiz) updatePromotion(ctx context.Context, params UpdatePromotionParams) (promotionmodel.PromotionBase, error) {
	var zero promotionmodel.PromotionBase

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// TODO: check more biz like unique code, valid owner, valid refID for the refType, dateStarted < dateEnded, etc.
	// dateEnded cannot less than dateStarted and current time

	var dbPromo db.PromotionBase

	if err := s.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error

		dbPromo, err = txStorage.UpdatePromotionBase(ctx, db.UpdatePromotionBaseParams{
			ID:            params.ID,
			Code:          pgutil.NullStringToPgText(params.Code),
			Title:         pgutil.NullStringToPgText(params.Title),
			Description:   pgutil.NullStringToPgText(params.Description),
			IsActive:      pgutil.NullBoolToPgBool(params.IsActive),
			AutoApply:     pgutil.NullBoolToPgBool(params.AutoApply),
			DateStarted:   pgutil.NullTimeToPgTimestamptz(params.DateStarted),
			NullDateEnded: params.NullDateEnded,
			DateUpdated:   pgutil.TimeToPgTimestamptz(time.Now()),
		})
		if err != nil {
			return err
		}

		if params.Refs != nil {
			// Remove all refs
			if err := txStorage.DeletePromotionRef(ctx, db.DeletePromotionRefParams{
				PromotionID: []int64{params.ID},
			}); err != nil {
				return err
			}

			// Add new refs
			if _, err = txStorage.CreateCopyDefaultPromotionRef(ctx, slice.Map(params.Refs, func(r PromotionRef) db.CreateCopyDefaultPromotionRefParams {
				return db.CreateCopyDefaultPromotionRefParams{
					PromotionID: dbPromo.ID,
					RefType:     r.RefType,
					RefID:       r.RefID,
				}
			})); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update promotion: %w", err)
	}

	return DbPromotionToPromotionBase(dbPromo, nil), nil
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

func DbPromotionToPromotionBase(dbPromo db.PromotionBase, refs []db.PromotionRef) promotionmodel.PromotionBase {
	return promotionmodel.PromotionBase{
		ID:          dbPromo.ID,
		Code:        dbPromo.Code,
		OwnerID:     pgutil.PgInt8ToNullInt64(dbPromo.OwnerID),
		Type:        dbPromo.Type,
		Title:       dbPromo.Title,
		Description: pgutil.PgTextToNullString(dbPromo.Description),
		IsActive:    dbPromo.IsActive,
		AutoApply:   dbPromo.AutoApply,
		DateStarted: dbPromo.DateStarted.Time,
		DateEnded:   pgutil.PgTimestamptzToNullTime(dbPromo.DateEnded),
		Refs: slice.Map(refs, func(r db.PromotionRef) promotionmodel.PromotionRef {
			return promotionmodel.PromotionRef{
				RefType: r.RefType,
				RefID:   r.RefID,
			}
		}),
		DateCreated: dbPromo.DateCreated.Time,
		DateUpdated: dbPromo.DateUpdated.Time,
	}
}
