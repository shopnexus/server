package orderbiz

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	accountmodel "shopnexus-server/internal/module/account/model"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListRefundsParams struct {
	sharedmodel.PaginationParams
}

// ListRefunds returns paginated refund requests with attached resources.
func (b *OrderBizHandler) ListRefunds(ctx restate.Context, params ListRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	return restate.Run(ctx, func(ctx restate.RunContext) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
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

		resourcesMap, err := b.common.GetResources(ctx, commonbiz.GetResourcesParams{
			RefType: commondb.CommonResourceRefTypeRefund,
			RefIDs:  ids,
		})
		if err != nil {
			return zero, err
		}

		return sharedmodel.PaginateResult[ordermodel.Refund]{
			PageParams: params.PaginationParams,
			Total:      total,
			Data: lo.Map(listCountRefund, func(r orderdb.ListCountRefundRow, _ int) ordermodel.Refund {
				m := dbToRefund(r.OrderRefund)
				m.Resources = resourcesMap[r.OrderRefund.ID]
				return m
			}),
		}, nil
	})
}

type CreateRefundParams struct {
	Account     accountmodel.AuthenticatedAccount
	OrderID     uuid.UUID                 `validate:"required"`
	Method      orderdb.OrderRefundMethod `validate:"required,validateFn=Valid"`
	Reason      string                    `validate:"required,max=500"`
	Address     null.String               `validate:"omitempty,max=500"`
	ResourceIDs []uuid.UUID               `validate:"dive"`
}

// CreateRefund creates a new refund request for an order and tracks refund analytics.
func (b *OrderBizHandler) CreateRefund(ctx restate.Context, params CreateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	if params.Method == orderdb.OrderRefundMethodPickUp && !params.Address.Valid {
		return zero, ordermodel.ErrRefundAddressRequired.Terminal()
	}

	// Create refund + update resources in one durable step
	refund, err := restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Refund, error) {
		// TODO: check if the order item belongs to the account
		// TODO: check if the order item is refundable (not refunded yet, within time limit, etc)

		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: params.OrderID, Valid: true})
		if err != nil {
			return zero, fmt.Errorf("get order: %w", err)
		}
		_ = order
		// TODO: check if the order is refundable

		dbRefund, err := b.storage.Querier().CreateDefaultRefund(ctx, orderdb.CreateDefaultRefundParams{
			AccountID: params.Account.ID,
			OrderID:   params.OrderID,
			Method:    params.Method,
			Reason:    params.Reason,
			Address:   params.Address,
		})
		if err != nil {
			return zero, fmt.Errorf("create refund: %w", err)
		}

		resources, err := b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Account:         params.Account,
			RefType:         commondb.CommonResourceRefTypeRefund,
			RefID:           dbRefund.ID,
			ResourceIDs:     params.ResourceIDs,
			EmptyResources:  false,
			DeleteResources: false,
		})
		if err != nil {
			return zero, fmt.Errorf("update refund resources: %w", err)
		}

		m := dbToRefund(dbRefund)
		m.Resources = resources
		return m, nil
	})
	if err != nil {
		return zero, err
	}

	// Track refund_requested interaction (separate step, uses GetOrder which has its own Run)
	if order, err := b.GetOrder(ctx, params.OrderID); err == nil {
		var refundInteractions []analyticbiz.CreateInteraction
		for _, item := range order.Items {
			refundInteractions = append(refundInteractions, analyticbiz.CreateInteraction{
				Account:   params.Account,
				EventType: analyticmodel.EventRefundReq,
				RefType:   analyticdb.AnalyticInteractionRefTypeProduct,
				RefID:     item.SkuID.String(),
			})
		}
		restate.ServiceSend(ctx, "AnalyticBiz", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
			Interactions: refundInteractions,
		})
	}

	return refund, nil
}

type UpdateRefundParams struct {
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

// UpdateRefund updates a pending refund's method, reason, address, or status.
func (b *OrderBizHandler) UpdateRefund(ctx restate.Context, params UpdateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	return restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Refund, error) {
		refund, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return zero, fmt.Errorf("get refund: %w", err)
		}

		if refund.Status != orderdb.OrderStatusPending {
			return zero, ordermodel.ErrRefundCannotBeUpdated.Terminal()
		}

		nullAddress := params.Method == orderdb.OrderRefundMethodDropOff

		refund, err = b.storage.Querier().UpdateRefund(ctx, orderdb.UpdateRefundParams{
			ID:            params.RefundID,
			Method:        orderdb.NullOrderRefundMethod{OrderRefundMethod: params.Method, Valid: params.Method != ""},
			Reason:        params.Reason,
			Address:       params.Address,
			NullAddress:   nullAddress,
			Status:        orderdb.NullOrderStatus{OrderStatus: params.Status, Valid: params.Status != ""},
			ConfirmedByID: params.ConfirmedByID,
		})
		if err != nil {
			return zero, fmt.Errorf("update refund: %w", err)
		}

		//TODO: use message queue instead of sequential processing
		resources, err := b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Account:         params.Account,
			RefType:         commondb.CommonResourceRefTypeRefund,
			RefID:           refund.ID,
			ResourceIDs:     params.ResourceIDs,
			DeleteResources: true,
		})
		if err != nil {
			return zero, fmt.Errorf("update refund resources: %w", err)
		}

		m := dbToRefund(refund)
		m.Resources = resources
		return m, nil
	})
}

// dbToRefund maps a DB OrderRefund row to the model type.
// Callers should set Resources as needed.
func dbToRefund(r orderdb.OrderRefund) ordermodel.Refund {
	return ordermodel.Refund{
		ID:            r.ID,
		AccountID:     r.AccountID,
		OrderID:       r.OrderID,
		ConfirmedByID: r.ConfirmedByID,
		ShipmentID:    r.ShipmentID,
		Method:        r.Method,
		Reason:        r.Reason,
		Address:       r.Address,
		Status:        r.Status,
		DateCreated:   r.DateCreated,
	}
}

type CancelRefundParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

// CancelRefund cancels a refund request by setting its status to canceled.
func (b *OrderBizHandler) CancelRefund(ctx restate.Context, params CancelRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		if _, err := b.storage.Querier().UpdateRefund(ctx, orderdb.UpdateRefundParams{
			ID:     params.RefundID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("cancel refund %s: %w", params.RefundID, err)
		}
		return nil
	})
}

type ConfirmRefundParams struct {
	Account  accountmodel.AuthenticatedAccount
	RefundID uuid.UUID `validate:"required"`
}

// ConfirmRefund marks a refund as confirmed by the vendor and transitions it to processing.
func (b *OrderBizHandler) ConfirmRefund(ctx restate.Context, params ConfirmRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// TODO: tell the shipment to take the refund package if method is pick-up, skip if drop-off

	return b.UpdateRefund(ctx, UpdateRefundParams{
		Account:       params.Account,
		RefundID:      params.RefundID,
		Status:        orderdb.OrderStatusProcessing,
		ConfirmedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
	})
}
