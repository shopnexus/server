package orderbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type CreateRefundParams struct {
	Account     authmodel.AuthenticatedAccount
	OrderItemID int64
	Method      db.OrderRefundMethod
	Reason      string
	Address     null.String
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
	Note     null.String
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

	var nullAddress bool
	if params.Method.Valid && params.Method.V == db.OrderRefundMethodDropOff {
		nullAddress = true
	}

	refund, err := txStorage.UpdateOrderRefund(ctx, db.UpdateOrderRefundParams{
		ID:          params.RefundID,
		Method:      db.NullOrderRefundMethod{OrderRefundMethod: params.Method.V, Valid: params.Method.Valid},
		Reason:      pgutil.NullStringToPgText(params.Reason),
		Address:     pgutil.NullStringToPgText(params.Address),
		NullAddress: nullAddress,
	})
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return refund, nil
}

type DeleteRefundParams struct {
	Account  authmodel.AuthenticatedAccount
	RefundID int64
}

func (b *OrderBiz) DeleteRefund(ctx context.Context, params DeleteRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	if err := txStorage.DeleteOrderRefund(ctx, db.DeleteOrderRefundParams{
		ID: []int64{params.RefundID},
	}); err != nil {
		return err
	}

	// Also remove all associated resources

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}
