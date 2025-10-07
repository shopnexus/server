package orderbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListRefundsParams struct {
	sharedmodel.PaginationParams
}

func (b *OrderBiz) ListRefunds(ctx context.Context, params ListRefundsParams) (sharedmodel.PaginateResult[db.OrderRefund], error) {
	var zero sharedmodel.PaginateResult[db.OrderRefund]

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

	return sharedmodel.PaginateResult[db.OrderRefund]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       refunds,
	}, nil
}

type CreateRefundParams struct {
	Account     authmodel.AuthenticatedAccount
	OrderItemID int64                `validate:"required"`
	Method      db.OrderRefundMethod `validate:"required,validFn=Valid"`
	Reason      string               `validate:"required,max=500"`
	Address     null.String          `validate:"omitempty,max=500"`
	// TODO: add images
}

func (b *OrderBiz) CreateRefund(ctx context.Context, params CreateRefundParams) (db.OrderRefund, error) {
	var zero db.OrderRefund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	if params.Method == db.OrderRefundMethodPickUp && !params.Address.Valid {
		return zero, ordermodel.ErrRefundAddressRequired
	}

	// TODO: check if the order item belongs to the account

	refund, err := txStorage.CreateOrderRefund(ctx, db.CreateOrderRefundParams{
		OrderItemID: params.OrderItemID,
		Status:      db.SharedStatusProcessing,
		Method:      params.Method,
		Reason:      params.Reason,
		Address:     pgutil.NullStringToPgText(params.Address),
	})

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return refund, nil
}

type UpdateRefundParams struct {
	Account  authmodel.AuthenticatedAccount
	RefundID int64
	Method   null.Value[db.OrderRefundMethod]
	Address  null.String
	Reason   null.String

	// Fields below are only updated after vendor confirms
	Status       null.Value[db.SharedStatus]
	ReviewedByID null.Int64
	// TODO: add images
}

func (b *OrderBiz) UpdateRefund(ctx context.Context, params UpdateRefundParams) (db.OrderRefund, error) {
	var zero db.OrderRefund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	refund, err := txStorage.GetOrderRefund(ctx, pgutil.Int64ToPgInt8(params.RefundID))
	if err != nil {
		return zero, err
	}

	// Only pending refunds can be updated
	if refund.Status != db.SharedStatusPending {
		return zero, ordermodel.ErrRefundCannotBeUpdated
	}

	var nullAddress bool
	if params.Method.Valid && params.Method.V == db.OrderRefundMethodDropOff {
		nullAddress = true
	}

	refund, err = txStorage.UpdateOrderRefund(ctx, db.UpdateOrderRefundParams{
		ID:          params.RefundID,
		Method:      db.NullOrderRefundMethod{OrderRefundMethod: params.Method.V, Valid: params.Method.Valid},
		Reason:      pgutil.NullStringToPgText(params.Reason),
		Address:     pgutil.NullStringToPgText(params.Address),
		NullAddress: nullAddress,

		// Update fields after vendor confirms
		Status:       db.NullSharedStatus{SharedStatus: params.Status.V, Valid: params.Status.Valid}, // Only vendor can update status
		ReviewedByID: pgutil.NullInt64ToPgInt8(params.ReviewedByID),
	})
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return refund, nil
}

type CancelRefundParams struct {
	Account  authmodel.AuthenticatedAccount
	RefundID int64 `validate:"required"`
}

func (b *OrderBiz) CancelRefund(ctx context.Context, params CancelRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	if _, err := txStorage.UpdateOrderRefund(ctx, db.UpdateOrderRefundParams{
		ID:     params.RefundID,
		Status: db.NullSharedStatus{SharedStatus: db.SharedStatusCanceled, Valid: true},
	}); err != nil {
		return err
	}

	//TODO: Also remove all associated resources

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}

type ConfirmRefundParams struct {
	Account  authmodel.AuthenticatedAccount
	RefundID int64 `validate:"required"`
}

func (b *OrderBiz) ConfirmRefund(ctx context.Context, params ConfirmRefundParams) (db.OrderRefund, error) {
	var zero db.OrderRefund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	return b.UpdateRefund(ctx, UpdateRefundParams{
		Account:      params.Account,
		RefundID:     params.RefundID,
		Status:       null.NewValue(db.SharedStatusProcessing, true),
		ReviewedByID: null.NewInt(params.Account.ID, true),
	})
}
