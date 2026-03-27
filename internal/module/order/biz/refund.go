package orderbiz

import (
	"encoding/json"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
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

// ListRefunds returns paginated refund requests with attached resources.
func (b *OrderHandler) ListRefunds(ctx restate.Context, params ListRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	if err := validator.Validate(params); err != nil {
		return zero, restate.TerminalErrorf("validate list refunds: %w", err)
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

// CreateRefund creates a new refund request for an order and tracks refund analytics.
func (b *OrderHandler) CreateRefund(ctx restate.Context, params CreateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, restate.TerminalErrorf("validate create refund: %w", err)
	}

	if params.Method == orderdb.OrderRefundMethodPickUp && !params.Address.Valid {
		return zero, ordermodel.ErrRefundAddressRequired.Terminal()
	}

	// Create refund + update resources in one durable step
	refund, err := restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Refund, error) {
		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: params.OrderID, Valid: true})
		if err != nil {
			return zero, fmt.Errorf("get order: %w", err)
		}
		_ = order

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
		return zero, sharedmodel.WrapErr("create refund", err)
	}

	// Track refund_requested interaction
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
		restate.ServiceSend(ctx, "Analytic", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
			Interactions: refundInteractions,
		})
	}

	return refund, nil
}

// UpdateRefund updates a pending refund's method, reason, address, or status.
func (b *OrderHandler) UpdateRefund(ctx restate.Context, params UpdateRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, restate.TerminalErrorf("validate update refund: %w", err)
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
func dbToRefund(r orderdb.OrderRefund) ordermodel.Refund {
	var confirmedByID *uuid.UUID
	if r.ConfirmedByID.Valid {
		confirmedByID = &r.ConfirmedByID.UUID
	}
	var transportID *uuid.UUID
	if r.TransportID.Valid {
		transportID = &r.TransportID.UUID
	}
	var address *string
	if r.Address.Valid {
		address = &r.Address.String
	}

	return ordermodel.Refund{
		ID:            r.ID,
		AccountID:     r.AccountID,
		OrderID:       r.OrderID,
		ConfirmedByID: confirmedByID,
		TransportID:   transportID,
		Method:        r.Method,
		Reason:        r.Reason,
		Address:       address,
		Status:        r.Status,
		DateCreated:   r.DateCreated,
	}
}

// CancelRefund cancels a refund request by setting its status to canceled.
func (b *OrderHandler) CancelRefund(ctx restate.Context, params CancelRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return restate.TerminalErrorf("validate cancel refund: %w", err)
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

// ConfirmRefund marks a refund as confirmed by the vendor and transitions it to processing.
func (b *OrderHandler) ConfirmRefund(ctx restate.Context, params ConfirmRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, restate.TerminalErrorf("validate confirm refund: %w", err)
	}

	refund, err := b.UpdateRefund(ctx, UpdateRefundParams{
		Account:       params.Account,
		RefundID:      params.RefundID,
		Status:        orderdb.OrderStatusProcessing,
		ConfirmedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("confirm refund", err)
	}

	// Notify customer: refund confirmed
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: refund.AccountID,
		Type:      "refund_approved",
		Channel:   "in_app",
		Title:     "Refund approved",
		Content:   fmt.Sprintf("Your refund request %s has been approved.", refund.ID),
		Metadata:  json.RawMessage(fmt.Sprintf(`{"order_id":"%s","refund_id":"%s"}`, refund.OrderID, refund.ID)),
	})

	return refund, nil
}
