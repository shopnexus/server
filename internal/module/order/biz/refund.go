package orderbiz

import (
	"context"
	"fmt"

	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	"shopnexus-remastered/internal/module/shared/validator"
	"shopnexus-remastered/internal/utils/pgsqlc"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type ListRefundsParams struct {
	commonmodel.PaginationParams
}

func (b *OrderBiz) ListRefunds(ctx context.Context, params ListRefundsParams) (commonmodel.PaginateResult[ordermodel.Refund], error) {
	var zero commonmodel.PaginateResult[ordermodel.Refund]

	storageParams := db.ListOrderRefundParams{
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
	}

	total, err := b.storage.CountOrderRefund(ctx, db.CountOrderRefundParams{})
	if err != nil {
		return zero, err
	}

	refunds, err := b.storage.ListOrderRefund(ctx, storageParams)
	if err != nil {
		return zero, err
	}

	resources, err := b.storage.ListSortedResources(ctx, db.ListSortedResourcesParams{
		RefType: db.CommonResourceRefTypeRefund,
		RefID:   slice.Map(refunds, func(r db.OrderRefund) int64 { return r.ID }),
	})
	if err != nil {
		return zero, err
	}
	resourceMap := slice.GroupBySlice(resources, func(r db.ListSortedResourcesRow) (int64, db.ListSortedResourcesRow) { return r.RefID, r })

	return commonmodel.PaginateResult[ordermodel.Refund]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data: slice.Map(refunds, func(refund db.OrderRefund) ordermodel.Refund {
			return ordermodel.Refund{
				ID:           refund.ID,
				AccountID:    refund.AccountID,
				OrderItemID:  refund.OrderItemID,
				Method:       refund.Method,
				Reason:       refund.Reason,
				Address:      pgutil.PgTextToNullString(refund.Address),
				Status:       refund.Status,
				ReviewedByID: pgutil.PgInt8ToNullInt64(refund.ReviewedByID),
				ShipmentID:   pgutil.PgInt8ToNullInt64(refund.ShipmentID),
				DateCreated:  refund.DateCreated.Time,
				Resources: slice.Map(resourceMap[refund.ID], func(resource db.ListSortedResourcesRow) commonmodel.Resource {
					return commonmodel.Resource{
						ID:   resource.ID.Bytes,
						Mime: resource.Mime,
						Url:  b.common.MustGetFileURL(ctx, resource.Provider, resource.ObjectKey),
						Size: resource.Size,
					}
				}),
			}
		}),
	}, nil
}

type CreateRefundParams struct {
	Storage     pgsqlc.Storage
	Account     authmodel.AuthenticatedAccount
	OrderItemID int64                `validate:"required"`
	Method      db.OrderRefundMethod `validate:"required,validateFn=Valid"`
	Reason      string               `validate:"required,max=500"`
	Address     null.String          `validate:"omitempty,max=500"`
	ResourceIDs []uuid.UUID          `validate:"dive"`
}

func (b *OrderBiz) CreateRefund(ctx context.Context, params CreateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	if params.Method == db.OrderRefundMethodPickUp && !params.Address.Valid {
		return zero, ordermodel.ErrRefundAddressRequired
	}

	// TODO: check if the order item belongs to the account
	// TODO: check if the order item is refundable (not refunded yet, within time limit, etc)

	var (
		refund    db.OrderRefund
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		orderItem, err := txStorage.GetOrderItem(ctx, pgutil.Int64ToPgInt8(params.OrderItemID))
		if err != nil {
			return fmt.Errorf("failed to get order item: %w", err)
		}
		if orderItem.Status != db.CommonStatusSuccess {
			return fmt.Errorf("cannot refund order item with status %s", orderItem.Status)
		}

		refund, err = txStorage.CreateDefaultOrderRefund(ctx, db.CreateDefaultOrderRefundParams{
			AccountID:   params.Account.ID,
			OrderItemID: params.OrderItemID,
			Method:      params.Method,
			Reason:      params.Reason,
			Address:     pgutil.NullStringToPgText(params.Address),
		})
		if err != nil {
			return fmt.Errorf("failed to create refund: %w", err)
		}

		var updateErr error
		resources, updateErr = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Storage:         txStorage,
			Account:         params.Account,
			RefType:         db.CommonResourceRefTypeRefund,
			RefID:           refund.ID,
			ResourceIDs:     params.ResourceIDs,
			EmptyResources:  false,
			DeleteResources: false,
		})
		if updateErr != nil {
			return fmt.Errorf("failed to update refund resources: %w", updateErr)
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create refund: %w", err)
	}

	return ordermodel.Refund{
		ID:           refund.ID,
		AccountID:    refund.AccountID,
		OrderItemID:  refund.OrderItemID,
		Method:       refund.Method,
		Reason:       refund.Reason,
		Address:      pgutil.PgTextToNullString(refund.Address),
		Status:       refund.Status,
		ReviewedByID: pgutil.PgInt8ToNullInt64(refund.ReviewedByID),
		ShipmentID:   pgutil.PgInt8ToNullInt64(refund.ShipmentID),
		DateCreated:  refund.DateCreated.Time,
		Resources:    resources,
	}, nil
}

type UpdateRefundParams struct {
	Storage  pgsqlc.Storage
	Account  authmodel.AuthenticatedAccount
	RefundID int64                `validate:"required"`
	Method   db.OrderRefundMethod `validate:"omitempty,validateFn=Valid"`
	Address  null.String          `validate:"omitnil,max=500"`
	Reason   null.String          `validate:"omitnil,max=500"`

	// Fields below are only updated after vendor confirms
	Status       db.CommonStatus `validate:"omitempty,validateFn=Valid"`
	ReviewedByID null.Int64      `validate:"omitnil,gt=0"`
	ResourceIDs  []uuid.UUID     `validate:"required,dive"`
}

func (b *OrderBiz) UpdateRefund(ctx context.Context, params UpdateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		refund    db.OrderRefund
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		refund, err = txStorage.GetOrderRefund(ctx, pgutil.Int64ToPgInt8(params.RefundID))
		if err != nil {
			return fmt.Errorf("failed to get refund: %w", err)
		}

		if refund.Status != db.CommonStatusPending {
			return ordermodel.ErrRefundCannotBeUpdated
		}

		nullAddress := params.Method == db.OrderRefundMethodDropOff

		refund, err = txStorage.UpdateOrderRefund(ctx, db.UpdateOrderRefundParams{
			ID:           params.RefundID,
			Method:       db.NullOrderRefundMethod{OrderRefundMethod: params.Method, Valid: params.Method != ""},
			Reason:       pgutil.NullStringToPgText(params.Reason),
			Address:      pgutil.NullStringToPgText(params.Address),
			NullAddress:  nullAddress,
			Status:       db.NullCommonStatus{CommonStatus: params.Status, Valid: params.Status != ""},
			ReviewedByID: pgutil.NullInt64ToPgInt8(params.ReviewedByID),
		})
		if err != nil {
			return fmt.Errorf("failed to update refund: %w", err)
		}

		resources, err = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Storage:         txStorage,
			Account:         params.Account,
			RefType:         db.CommonResourceRefTypeRefund,
			RefID:           refund.ID,
			ResourceIDs:     params.ResourceIDs,
			DeleteResources: true,
		})
		if err != nil {
			return fmt.Errorf("failed to update refund resources: %w", err)
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to update refund: %w", err)
	}

	return ordermodel.Refund{
		ID:           refund.ID,
		AccountID:    refund.AccountID,
		OrderItemID:  refund.OrderItemID,
		Method:       refund.Method,
		Reason:       refund.Reason,
		Address:      pgutil.PgTextToNullString(refund.Address),
		Status:       refund.Status,
		ReviewedByID: pgutil.PgInt8ToNullInt64(refund.ReviewedByID),
		ShipmentID:   pgutil.PgInt8ToNullInt64(refund.ShipmentID),
		DateCreated:  refund.DateCreated.Time,
		Resources:    resources,
	}, nil
}

type CancelRefundParams struct {
	Storage  pgsqlc.Storage
	Account  authmodel.AuthenticatedAccount
	RefundID int64 `validate:"required"`
}

func (b *OrderBiz) CancelRefund(ctx context.Context, params CancelRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		if _, err := txStorage.UpdateOrderRefund(ctx, db.UpdateOrderRefundParams{
			ID:     params.RefundID,
			Status: db.NullCommonStatus{CommonStatus: db.CommonStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("failed to cancel refund: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to cancel refund %d: %w", params.RefundID, err)
	}

	return nil
}

type ConfirmRefundParams struct {
	Storage  pgsqlc.Storage
	Account  authmodel.AuthenticatedAccount
	RefundID int64 `validate:"required"`
}

func (b *OrderBiz) ConfirmRefund(ctx context.Context, params ConfirmRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// TODO: tell the shipment to take the refund package if method is pick-up, skip if drop-off

	return b.UpdateRefund(ctx, UpdateRefundParams{
		Storage:      params.Storage,
		Account:      params.Account,
		RefundID:     params.RefundID,
		Status:       db.CommonStatusProcessing,
		ReviewedByID: null.NewInt(params.Account.ID, true),
	})
}
