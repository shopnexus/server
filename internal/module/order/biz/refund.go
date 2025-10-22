package orderbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
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

func (b *OrderBiz) ListRefunds(ctx context.Context, params ListRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

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
		RefType: db.SharedResourceRefTypeRefund,
		RefID:   slice.Map(refunds, func(r db.OrderRefund) int64 { return r.ID }),
	})
	if err != nil {
		return zero, err
	}
	resourceMap := slice.GroupBySlice(resources, func(r db.ListSortedResourcesRow) (int64, db.ListSortedResourcesRow) { return r.RefID, r })

	return sharedmodel.PaginateResult[ordermodel.Refund]{
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
				Resources: slice.Map(resourceMap[refund.ID], func(resource db.ListSortedResourcesRow) sharedmodel.Resource {
					return sharedmodel.Resource{
						ID:       resource.ID,
						Mime:     resource.Mime,
						Url:      sharedbiz.GetResourceURL(string(resource.Provider), resource.ObjectKey),
						FileSize: pgutil.PgInt8ToNullInt64(resource.FileSize),
						Width:    pgutil.PgInt4ToNullInt32(resource.Width),
						Height:   pgutil.PgInt4ToNullInt32(resource.Height),
						Duration: pgutil.PgFloat8ToNullFloat(resource.Duration),
					}
				}),
			}
		}),
	}, nil
}

type CreateRefundParams struct {
	Account     authmodel.AuthenticatedAccount
	OrderItemID int64                `validate:"required"`
	Method      db.OrderRefundMethod `validate:"required,validateFn=Valid"`
	Reason      string               `validate:"required,max=500"`
	Address     null.String          `validate:"omitempty,max=500"`
	ResourceIDs []int64              `validate:"dive"`
}

func (b *OrderBiz) CreateRefund(ctx context.Context, params CreateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

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
	// TODO: check if the order item is refundable (not refunded yet, within time limit, etc)

	orderItem, err := txStorage.GetOrderItem(ctx, pgutil.Int64ToPgInt8(params.OrderItemID))
	if err != nil {
		return zero, err
	}
	if orderItem.Status != db.SharedStatusSuccess {
		return zero, fmt.Errorf("cannot refund order item with status %s", orderItem.Status)
	}

	refund, err := txStorage.CreateDefaultOrderRefund(ctx, db.CreateDefaultOrderRefundParams{
		AccountID:   params.Account.ID,
		OrderItemID: params.OrderItemID,
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
		ID:         params.ResourceIDs,
		UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
	})
	if err != nil {
		return zero, err
	}
	if len(resources) != len(params.ResourceIDs) {
		// Some resources not found or not belong to the user
		return zero, sharedmodel.ErrResourceNotFound
	}

	for order, rsID := range params.ResourceIDs {
		createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceReferenceParams{
			RsID:      rsID,
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
		Resources: slice.Map(resources, func(resource db.SharedResource) sharedmodel.Resource {
			return sharedmodel.Resource{
				ID:       resource.ID,
				Mime:     resource.Mime,
				Url:      sharedbiz.GetResourceURL(string(resource.Provider), resource.ObjectKey),
				FileSize: pgutil.PgInt8ToNullInt64(resource.FileSize),
				Width:    pgutil.PgInt4ToNullInt32(resource.Width),
				Height:   pgutil.PgInt4ToNullInt32(resource.Height),
				Duration: pgutil.PgFloat8ToNullFloat(resource.Duration),
			}
		}),
	}, nil
}

type UpdateRefundParams struct {
	Account  authmodel.AuthenticatedAccount
	RefundID int64                `validate:"required"`
	Method   db.OrderRefundMethod `validate:"omitempty,validateFn=Valid"`
	Address  null.String          `validate:"omitnil,max=500"`
	Reason   null.String          `validate:"omitnil,max=500"`

	// Fields below are only updated after vendor confirms
	Status       db.SharedStatus `validate:"omitempty,validateFn=Valid"`
	ReviewedByID null.Int64      `validate:"omitnil,gt=0"`
	ResourceIDs  []int64         `validate:"omitempty,dive"`
}

func (b *OrderBiz) UpdateRefund(ctx context.Context, params UpdateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

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
	if params.Method == db.OrderRefundMethodDropOff {
		nullAddress = true
	}

	refund, err = txStorage.UpdateOrderRefund(ctx, db.UpdateOrderRefundParams{
		ID:          params.RefundID,
		Method:      db.NullOrderRefundMethod{OrderRefundMethod: params.Method, Valid: params.Method != ""},
		Reason:      pgutil.NullStringToPgText(params.Reason),
		Address:     pgutil.NullStringToPgText(params.Address),
		NullAddress: nullAddress,

		// Update fields after vendor confirms
		Status:       db.NullSharedStatus{SharedStatus: params.Status, Valid: params.Status != ""},
		ReviewedByID: pgutil.NullInt64ToPgInt8(params.ReviewedByID),
	})
	if err != nil {
		return zero, err
	}

	// TODO: shorten the update resource, create reuseable function
	// Update resources
	var resources []db.SharedResource
	if len(params.ResourceIDs) > 0 {
		// Delete old resources
		if err := txStorage.DeleteSharedResourceReference(ctx, db.DeleteSharedResourceReferenceParams{
			RefType: []db.SharedResourceRefType{db.SharedResourceRefTypeRefund},
			RefID:   []int64{params.RefundID},
		}); err != nil {
			return zero, err
		}

		// Attach resources

		var createResourceArgs []db.CreateCopyDefaultSharedResourceReferenceParams

		resources, err = txStorage.ListSharedResource(ctx, db.ListSharedResourceParams{
			ID:         params.ResourceIDs,
			UploadedBy: []pgtype.Int8{{Int64: params.Account.ID, Valid: true}}, // Can only attach own uploaded resources
		})
		if err != nil {
			return zero, err
		}
		if len(resources) != len(params.ResourceIDs) {
			// Some resources not found or not belong to the user
			return zero, sharedmodel.ErrResourceNotFound
		}

		for order, rsID := range params.ResourceIDs {
			createResourceArgs = append(createResourceArgs, db.CreateCopyDefaultSharedResourceReferenceParams{
				RsID:      rsID,
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
		Resources: slice.Map(resources, func(resource db.SharedResource) sharedmodel.Resource {
			return sharedmodel.Resource{
				ID:       resource.ID,
				Mime:     resource.Mime,
				Url:      sharedbiz.GetResourceURL(string(resource.Provider), resource.ObjectKey),
				FileSize: pgutil.PgInt8ToNullInt64(resource.FileSize),
				Width:    pgutil.PgInt4ToNullInt32(resource.Width),
				Height:   pgutil.PgInt4ToNullInt32(resource.Height),
				Duration: pgutil.PgFloat8ToNullFloat(resource.Duration),
			}
		}),
	}, nil
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

func (b *OrderBiz) ConfirmRefund(ctx context.Context, params ConfirmRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	return b.UpdateRefund(ctx, UpdateRefundParams{
		Account:      params.Account,
		RefundID:     params.RefundID,
		Status:       db.SharedStatusProcessing,
		ReviewedByID: null.NewInt(params.Account.ID, true),
	})
}
