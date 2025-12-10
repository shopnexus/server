package orderbiz

import (
	"context"
	"fmt"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commondb "shopnexus-remastered/internal/module/common/db/sqlc"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListRefundsParams struct {
	sharedmodel.PaginationParams
}

func (b *OrderBiz) ListRefunds(ctx context.Context, params ListRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listCountRefund, err := b.storage.Querier().ListCountRefund(ctx, orderdb.ListCountRefundParams{
		Offset: params.Offset(),
		Limit:  params.Limit,
	})
	if err != nil {
		return zero, err
	}

	var total null.Int64
	if len(listCountRefund) > 0 {
		total.SetValid(listCountRefund[0].TotalCount)
	}

	ids := lo.Map(listCountRefund, func(refund orderdb.ListCountRefundRow, _ int) uuid.UUID {
		return refund.OrderRefund.ID
	})

	resourcesMap, err := b.common.GetResources(ctx, commondb.CommonResourceRefTypeRefund, ids)
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[ordermodel.Refund]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data: lo.Map(listCountRefund, func(r orderdb.ListCountRefundRow, _ int) ordermodel.Refund {
			refund := r.OrderRefund
			return ordermodel.Refund{
				ID:            refund.ID,
				AccountID:     refund.AccountID,
				OrderID:       refund.OrderID,
				ConfirmedByID: refund.ConfirmedByID,
				ShipmentID:    refund.ShipmentID,
				Method:        refund.Method,
				Reason:        refund.Reason,
				Address:       refund.Address,
				Status:        refund.Status,
				DateCreated:   refund.DateCreated,
				Resources:     resourcesMap[refund.ID],
			}
		}),
	}, nil
}

type CreateRefundParams struct {
	Storage     OrderStorage
	Account     accountmodel.AuthenticatedAccount
	OrderID     uuid.UUID                 `validate:"required"`
	Method      orderdb.OrderRefundMethod `validate:"required,validateFn=Valid"`
	Reason      string                    `validate:"required,max=500"`
	Address     null.String               `validate:"omitempty,max=500"`
	ResourceIDs []uuid.UUID               `validate:"dive"`
}

func (b *OrderBiz) CreateRefund(ctx context.Context, params CreateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	if params.Method == orderdb.OrderRefundMethodPickUp && !params.Address.Valid {
		return zero, ordermodel.ErrRefundAddressRequired
	}

	// TODO: check if the order item belongs to the account
	// TODO: check if the order item is refundable (not refunded yet, within time limit, etc)

	var (
		refund    orderdb.OrderRefund
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage OrderStorage) error {
		order, err := txStorage.Querier().GetOrder(ctx, orderdb.GetOrderParams{
			ID: uuid.NullUUID{UUID: params.OrderID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("failed to get order: %w", err)
		}
		_ = order
		// TODO: check if the order is refundable

		refund, err = txStorage.Querier().CreateDefaultRefund(ctx, orderdb.CreateDefaultRefundParams{
			AccountID: params.Account.ID,
			OrderID:   params.OrderID,
			Method:    params.Method,
			Reason:    params.Reason,
			Address:   params.Address,
		})
		if err != nil {
			return fmt.Errorf("failed to create refund: %w", err)
		}

		var updateErr error
		resources, updateErr = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			// Storage:         txStorage,
			Account:         params.Account,
			RefType:         commondb.CommonResourceRefTypeRefund,
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
		ID:            refund.ID,
		AccountID:     refund.AccountID,
		OrderID:       refund.OrderID,
		ConfirmedByID: refund.ConfirmedByID,
		ShipmentID:    refund.ShipmentID,
		Method:        refund.Method,
		Reason:        refund.Reason,
		Address:       refund.Address,
		Status:        refund.Status,
		DateCreated:   refund.DateCreated,
		Resources:     resources,
	}, nil
}

type UpdateRefundParams struct {
	Storage  OrderStorage
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID                 `validate:"required"`
	Method   orderdb.OrderRefundMethod `validate:"omitempty,validateFn=Valid"`
	Address  null.String               `validate:"omitnil,max=500"`
	Reason   null.String               `validate:"omitnil,max=500"`

	// Fields below are only updated after vendor confirms
	Status        orderdb.OrderStatus `validate:"omitempty,validateFn=Valid"`
	ConfirmedByID uuid.NullUUID       `validate:"omitnil"`
	ResourceIDs   []uuid.UUID         `validate:"required,dive"`
}

func (b *OrderBiz) UpdateRefund(ctx context.Context, params UpdateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	var (
		refund    orderdb.OrderRefund
		resources []commonmodel.Resource
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage OrderStorage) error {
		var err error
		refund, err = txStorage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return fmt.Errorf("failed to get refund: %w", err)
		}

		if refund.Status != orderdb.OrderStatusPending {
			return ordermodel.ErrRefundCannotBeUpdated
		}

		nullAddress := params.Method == orderdb.OrderRefundMethodDropOff

		refund, err = txStorage.Querier().UpdateRefund(ctx, orderdb.UpdateRefundParams{
			ID:            params.RefundID,
			Method:        orderdb.NullOrderRefundMethod{OrderRefundMethod: params.Method, Valid: params.Method != ""},
			Reason:        params.Reason,
			Address:       params.Address,
			NullAddress:   nullAddress,
			Status:        orderdb.NullOrderStatus{OrderStatus: params.Status, Valid: params.Status != ""},
			ConfirmedByID: params.ConfirmedByID,
		})
		if err != nil {
			return fmt.Errorf("failed to update refund: %w", err)
		}

		//TODO: use message queue instead of sequential processing
		resources, err = b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Storage:         pgsqlc.NewStorage(txStorage.Conn(), commondb.New(txStorage.Conn())),
			Account:         params.Account,
			RefType:         commondb.CommonResourceRefTypeRefund,
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
		ID:            refund.ID,
		AccountID:     refund.AccountID,
		OrderID:       refund.OrderID,
		Method:        refund.Method,
		Reason:        refund.Reason,
		Address:       refund.Address,
		Status:        refund.Status,
		ConfirmedByID: refund.ConfirmedByID,
		ShipmentID:    refund.ShipmentID,
		DateCreated:   refund.DateCreated,
		Resources:     resources,
	}, nil
}

type CancelRefundParams struct {
	Storage  OrderStorage
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

func (b *OrderBiz) CancelRefund(ctx context.Context, params CancelRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage OrderStorage) error {
		if _, err := txStorage.Querier().UpdateRefund(ctx, orderdb.UpdateRefundParams{
			ID:     params.RefundID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("failed to cancel refund: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to cancel refund %s: %w", params.RefundID, err)
	}

	return nil
}

type ConfirmRefundParams struct {
	Storage  OrderStorage
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

func (b *OrderBiz) ConfirmRefund(ctx context.Context, params ConfirmRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// TODO: tell the shipment to take the refund package if method is pick-up, skip if drop-off

	return b.UpdateRefund(ctx, UpdateRefundParams{
		Storage:       params.Storage,
		Account:       params.Account,
		RefundID:      params.RefundID,
		Status:        orderdb.OrderStatusProcessing,
		ConfirmedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
	})
}
