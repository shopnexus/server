package promotionbiz

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	promotiondb "shopnexus-remastered/internal/module/promotion/db/sqlc"
	promotionmodel "shopnexus-remastered/internal/module/promotion/model"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/validator"
)

type PromotionStorage = pgsqlc.Storage[*promotiondb.Queries]

type PromotionBiz struct {
	storage PromotionStorage
}

func NewPromotionBiz(storage PromotionStorage) *PromotionBiz {
	return &PromotionBiz{
		storage,
	}
}

type GetPromotionParams struct {
	ID uuid.UUID `validate:"required"`
}

func (s *PromotionBiz) GetPromotion(ctx context.Context, params GetPromotionParams) (promotiondb.PromotionPromotion, error) {
	promo, err := s.storage.Querier().GetPromotion(ctx, promotiondb.GetPromotionParams{
		ID: uuid.NullUUID{UUID: params.ID, Valid: true},
	})
	if err != nil {
		return promotiondb.PromotionPromotion{}, err
	}

	return promo, nil
}

type ListPromotionParams struct {
	commonmodel.PaginationParams
	ID []uuid.UUID `validate:"omitempty,dive,required"`
}

func (s *PromotionBiz) ListPromotion(ctx context.Context, params ListPromotionParams) (commonmodel.PaginateResult[promotionmodel.Promotion], error) {
	var zero commonmodel.PaginateResult[promotionmodel.Promotion]

	listCountPromotion, err := s.storage.Querier().ListCountPromotion(ctx, promotiondb.ListCountPromotionParams{
		Limit:  params.Limit,
		Offset: params.Offset(),
		ID:     params.ID,
	})
	if err != nil {
		return zero, err
	}

	refs, err := s.storage.Querier().ListRef(ctx, promotiondb.ListRefParams{
		PromotionID: lo.Map(listCountPromotion, func(p promotiondb.ListCountPromotionRow, _ int) uuid.UUID {
			return p.PromotionPromotion.ID
		}),
	})
	if err != nil {
		return zero, err
	}
	refsMap := lo.GroupBy(refs, func(r promotiondb.PromotionRef) uuid.UUID { return r.PromotionID })

	var total null.Int64
	if len(listCountPromotion) > 0 {
		total.SetValid(listCountPromotion[0].TotalCount)
	}

	return commonmodel.PaginateResult[promotionmodel.Promotion]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data: lo.Map(listCountPromotion, func(p promotiondb.ListCountPromotionRow, _ int) promotionmodel.Promotion {
			return DbPromotionToPromotion(p.PromotionPromotion, refsMap[p.PromotionPromotion.ID])
		}),
	}, nil
}

type CreatePromotionParams struct {
	Storage PromotionStorage
	Account accountmodel.AuthenticatedAccount

	Code        string                    `validate:"required,alphanum,min=3,max=50"`
	Refs        []PromotionRef            `validate:"dive"`
	Type        promotiondb.PromotionType `validate:"required,validateFn=Valid"`
	Title       string                    `validate:"required,min=3,max=200"`
	Description null.String               `validate:"omitnil,max=1000"`
	IsActive    bool                      `validate:"omitempty"`
	AutoApply   bool                      `validate:"omitempty"`
	DateStarted time.Time                 `validate:"required"`
	DateEnded   null.Time                 `validate:"omitnil,gtfield=DateStarted"`
}

type PromotionRef struct {
	RefType promotiondb.PromotionRefType `validate:"required,validateFn=Valid"`
	RefID   uuid.UUID                    `validate:"required"`
}

func (b *PromotionBiz) createPromotion(ctx context.Context, params CreatePromotionParams) (promotionmodel.Promotion, error) {
	var zero promotionmodel.Promotion

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var dbPromo promotiondb.PromotionPromotion

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage PromotionStorage) error {
		var err error

		dbPromo, err := txStorage.Querier().CreateDefaultPromotion(ctx, promotiondb.CreateDefaultPromotionParams{
			Code:        params.Code,
			OwnerID:     uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			Type:        promotiondb.PromotionTypeDiscount,
			Title:       params.Title,
			Description: params.Description,
			IsActive:    params.IsActive,
			AutoApply:   params.AutoApply,
			DateStarted: params.DateStarted,
			DateEnded:   params.DateEnded,
		})
		if err != nil {
			return err
		}

		_, err = txStorage.Querier().CreateCopyDefaultRef(ctx, lo.Map(params.Refs, func(r PromotionRef, _ int) promotiondb.CreateCopyDefaultRefParams {
			return promotiondb.CreateCopyDefaultRefParams{
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

	return DbPromotionToPromotion(dbPromo, nil), nil
}

type UpdatePromotionParams struct {
	Storage PromotionStorage
	Account accountmodel.AuthenticatedAccount

	ID            uuid.UUID      `validate:"required"`
	Code          null.String    `validate:"omitnil"`
	OwnerID       uuid.NullUUID  `validate:"omitnil"`
	Title         null.String    `validate:"omitnil"`
	Description   null.String    `validate:"omitnil"`
	IsActive      null.Bool      `validate:"omitnil"`
	AutoApply     null.Bool      `validate:"omitnil"`
	DateStarted   null.Time      `validate:"omitnil"`
	DateEnded     null.Time      `validate:"omitnil"`
	NullDateEnded bool           `validate:"omitempty"`
	Refs          []PromotionRef `validate:"dive"`
}

func (s *PromotionBiz) updatePromotion(ctx context.Context, params UpdatePromotionParams) (promotionmodel.Promotion, error) {
	var zero promotionmodel.Promotion

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// TODO: check more biz like unique code, valid owner, valid refID for the refType, dateStarted < dateEnded, etc.
	// dateEnded cannot less than dateStarted and current time

	var dbPromo promotiondb.PromotionPromotion

	if err := s.storage.WithTx(ctx, params.Storage, func(txStorage PromotionStorage) error {
		var err error

		dbPromo, err = txStorage.Querier().UpdatePromotion(ctx, promotiondb.UpdatePromotionParams{
			ID:            params.ID,
			Code:          params.Code,
			Title:         params.Title,
			Description:   params.Description,
			IsActive:      params.IsActive,
			AutoApply:     params.AutoApply,
			DateStarted:   params.DateStarted,
			NullDateEnded: params.NullDateEnded,
		})
		if err != nil {
			return err
		}

		if params.Refs != nil {
			// Remove all refs
			if err := txStorage.Querier().DeleteRef(ctx, promotiondb.DeleteRefParams{
				PromotionID: []uuid.UUID{params.ID},
			}); err != nil {
				return err
			}

			// Add new refs
			if _, err = txStorage.Querier().CreateCopyDefaultRef(ctx, lo.Map(params.Refs, func(r PromotionRef, _ int) promotiondb.CreateCopyDefaultRefParams {
				return promotiondb.CreateCopyDefaultRefParams{
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

	return DbPromotionToPromotion(dbPromo, nil), nil
}

type DeletePromotionParams struct {
	Account accountmodel.AuthenticatedAccount
	ID      uuid.UUID
}

func (s *PromotionBiz) DeletePromotion(ctx context.Context, params DeletePromotionParams) error {
	return s.storage.Querier().DeletePromotion(ctx, promotiondb.DeletePromotionParams{
		ID: []uuid.UUID{params.ID},
	})
}

func DbPromotionToPromotion(dbPromo promotiondb.PromotionPromotion, refs []promotiondb.PromotionRef) promotionmodel.Promotion {
	return promotionmodel.Promotion{
		ID:          dbPromo.ID,
		Code:        dbPromo.Code,
		OwnerID:     dbPromo.OwnerID,
		Type:        dbPromo.Type,
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
