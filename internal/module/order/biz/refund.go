package orderbiz

import (
	"context"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
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
	OrderItemID int64                        `validate:"required"`
	Method      db.OrderRefundMethod         `validate:"required,validateFn=Valid"`
	Reason      string                       `validate:"required,max=500"`
	Address     null.String                  `validate:"omitempty,max=500"`
	Resources   []sharedmodel.CreateResource `validate:"required,dive"`
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
	if err != nil {
		return zero, err
	}

	// Associate resources
	var createResourceArgs []db.CreateCopyDefaultSharedResourceReferenceParams

	resources, err := txStorage.ListSharedResource(ctx, db.ListSharedResourceParams{
		ID:         slice.Map(params.Resources, func(r sharedmodel.CreateResource) int64 { return r.FileID }),
		UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
	})
	if err != nil {
		return zero, err
	}
	if len(resources) != len(params.Resources) {
		// Some resources not found or not belong to the user
		return zero, sharedmodel.ErrResourceNotFound
	}

	for order, res := range params.Resources {
		createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceReferenceParams{
			RsID:      res.FileID,
			RefType:   db.SharedResourceRefTypeRefund,
			RefID:     refund.ID,
			Order:     int32(order),
			IsPrimary: false,
		})

		if _, err := txStorage.CreateCopyDefaultSharedResourceReference(ctx, createResourceArgs); err != nil {
			return zero, err
		}
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return refund, nil
}

type UpdateRefundParams struct {
	Account  authmodel.AuthenticatedAccount
	RefundID int64                            `validate:"required"`
	Method   null.Value[db.OrderRefundMethod] `validate:"omitnil,validateFn=Valid"`
	Address  null.String                      `validate:"omitnil,max=500"`
	Reason   null.String                      `validate:"omitnil,max=500"`

	// Fields below are only updated after vendor confirms
	Status       null.Value[db.SharedStatus]  `validate:"omitnil,validateFn=Valid"`
	ReviewedByID null.Int64                   `validate:"omitnil,gt=0"`
	Resources    []sharedmodel.CreateResource `validate:"omitempty,dive"`
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

	// TODO: shorten the update resource, create reuseable function
	// Update resources
	if len(params.Resources) > 0 {
		// Delete old resources
		if err := txStorage.DeleteSharedResourceReference(ctx, db.DeleteSharedResourceReferenceParams{
			RefType: []db.SharedResourceRefType{db.SharedResourceRefTypeRefund},
			RefID:   []int64{params.RefundID},
		}); err != nil {
			return zero, err
		}

		// Attach resources

		var createResourceArgs []db.CreateCopyDefaultSharedResourceReferenceParams

		resources, err := txStorage.ListSharedResource(ctx, db.ListSharedResourceParams{
			ID:         slice.Map(params.Resources, func(r sharedmodel.CreateResource) int64 { return r.FileID }),
			UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
		})
		if err != nil {
			return zero, err
		}
		if len(resources) != len(params.Resources) {
			// Some resources not found or not belong to the user
			return zero, sharedmodel.ErrResourceNotFound
		}

		for order, res := range params.Resources {
			createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceReferenceParams{
				RsID:      res.FileID,
				RefType:   db.SharedResourceRefTypeRefund,
				RefID:     refund.ID,
				Order:     int32(order),
				IsPrimary: false,
			})

			if _, err := txStorage.CreateCopyDefaultSharedResourceReference(ctx, createResourceArgs); err != nil {
				return zero, err
			}
		}
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
