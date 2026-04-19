package orderbiz

import (
	"encoding/json"
	"fmt"
	"log/slog"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// ListBuyerRefunds returns paginated refund requests with attached resources.
// TODO: add casbin authorization — filter by caller's account_id
func (b *OrderHandler) ListBuyerRefunds(
	ctx restate.Context,
	params ListBuyerRefundsParams,
) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list refunds", err)
	}

	type listResult struct {
		Refunds []orderdb.ListCountRefundRow `json:"refunds"`
	}
	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (listResult, error) {
		rows, err := b.storage.Querier().ListCountRefund(ctx, orderdb.ListCountRefundParams{
			Offset: params.Offset(),
			Limit:  params.Limit,
		})
		if err != nil {
			return listResult{}, err
		}
		return listResult{Refunds: rows}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list refunds", err)
	}

	var total null.Int64
	if len(dbResult.Refunds) > 0 {
		total.SetValid(dbResult.Refunds[0].TotalCount)
	}

	ids := lo.Map(dbResult.Refunds, func(r orderdb.ListCountRefundRow, _ int) uuid.UUID {
		return r.OrderRefund.ID
	})

	resourcesMap, err := b.common.GetResources(ctx, commonbiz.GetResourcesParams{
		RefType: commondb.CommonResourceRefTypeRefund,
		RefIDs:  ids,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get refund resources", err)
	}

	return sharedmodel.PaginateResult[ordermodel.Refund]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data: lo.Map(dbResult.Refunds, func(r orderdb.ListCountRefundRow, _ int) ordermodel.Refund {
			m := dbToRefund(r.OrderRefund)
			m.Resources = resourcesMap[r.OrderRefund.ID]
			return m
		}),
	}, nil
}

// CreateBuyerRefund creates a new refund request for an order and tracks refund analytics.
func (b *OrderHandler) CreateBuyerRefund(
	ctx restate.Context,
	params CreateBuyerRefundParams,
) (_ ordermodel.Refund, err error) {
	defer metrics.TrackHandler("order", "CreateBuyerRefund", &err)()

	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate create refund", err)
	}

	if params.Method == orderdb.OrderRefundMethodPickUp && !params.Address.Valid {
		return zero, ordermodel.ErrRefundAddressRequired.Terminal()
	}

	// Validate order exists and belongs to the buyer
	order, err := b.GetBuyerOrder(ctx, params.OrderID)
	if err != nil {
		return zero, sharedmodel.WrapErr("get order for refund", err)
	}
	if order.BuyerID != params.Account.ID {
		return zero, ordermodel.ErrOrderNotFound.Terminal()
	}

	// Partial refund: serialize item_ids to JSONB (null = full refund)
	var itemIdsJSON json.RawMessage
	if len(params.ItemIDs) > 0 {
		// Reject duplicates
		seen := make(map[int64]bool, len(params.ItemIDs))
		for _, id := range params.ItemIDs {
			if seen[id] {
				return zero, ordermodel.ErrRefundDuplicateItem.Terminal()
			}
			seen[id] = true
		}
		itemIdsJSON, _ = json.Marshal(params.ItemIDs)
	}

	// Partial amount: wrap in null.Int (null = compute from order total)
	var amount null.Int
	if params.Amount > 0 {
		amount = null.IntFrom(params.Amount)
	}

	// Create refund in durable step
	dbRefund, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderRefund, error) {
		return b.storage.Querier().CreateDefaultRefund(ctx, orderdb.CreateDefaultRefundParams{
			AccountID: params.Account.ID,
			OrderID:   params.OrderID,
			Method:    params.Method,
			Reason:    params.Reason,
			Address:   params.Address,
			ItemIds:   itemIdsJSON,
			Amount:    amount,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db create refund", err)
	}

	metrics.RefundsCreatedTotal.Inc()

	// Update resources outside Run (cross-module Restate call)
	resources, err := b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
		Account:         params.Account,
		RefType:         commondb.CommonResourceRefTypeRefund,
		RefID:           dbRefund.ID,
		ResourceIDs:     params.ResourceIDs,
		EmptyResources:  false,
		DeleteResources: false,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update refund resources", err)
	}

	refund := dbToRefund(dbRefund)
	refund.Resources = resources

	// Track refund_requested interaction + notify seller
	if order, err := b.GetBuyerOrder(ctx, params.OrderID); err == nil {
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

		// Notify seller: refund requested
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: order.SellerID,
			Type:      accountmodel.NotiRefundRequested,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Refund requested",
			Content:   fmt.Sprintf("A refund has been requested for %s.", ordermodel.SummarizeItems(order.Items)),
			Metadata:  json.RawMessage(fmt.Sprintf(`{"order_id":"%s","refund_id":"%s"}`, refund.OrderID, refund.ID)),
		})
	}

	return refund, nil
}

// UpdateBuyerRefund updates a pending refund's method, reason, address, or status.
// TODO: add casbin authorization — verify caller owns this refund
func (b *OrderHandler) UpdateBuyerRefund(
	ctx restate.Context,
	params UpdateBuyerRefundParams,
) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate update refund", err)
	}

	updatedRefund, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderRefund, error) {
		refund, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return orderdb.OrderRefund{}, sharedmodel.WrapErr("db get refund", err)
		}

		if refund.Status != orderdb.OrderStatusPending {
			return orderdb.OrderRefund{}, ordermodel.ErrRefundCannotBeUpdated.Terminal()
		}

		nullAddress := params.Method == orderdb.OrderRefundMethodDropOff

		return b.storage.Querier().UpdateRefund(ctx, orderdb.UpdateRefundParams{
			ID:            params.RefundID,
			Method:        orderdb.NullOrderRefundMethod{OrderRefundMethod: params.Method, Valid: params.Method != ""},
			Reason:        params.Reason,
			Address:       params.Address,
			NullAddress:   nullAddress,
			Status:        orderdb.NullOrderStatus{OrderStatus: params.Status, Valid: params.Status != ""},
			ConfirmedByID: params.ConfirmedByID,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db update refund", err)
	}

	// Update resources outside Run (cross-module Restate call)
	resources, err := b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
		Account:         params.Account,
		RefType:         commondb.CommonResourceRefTypeRefund,
		RefID:           updatedRefund.ID,
		ResourceIDs:     params.ResourceIDs,
		DeleteResources: true,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("update refund resources", err)
	}

	m := dbToRefund(updatedRefund)
	m.Resources = resources
	return m, nil
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

	refund := ordermodel.Refund{
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

	if r.ItemIds != nil {
		_ = json.Unmarshal(r.ItemIds, &refund.ItemIDs)
	}
	if r.Amount.Valid {
		refund.Amount = r.Amount.Int64
	}

	return refund
}

// CancelBuyerRefund cancels a refund request by setting its status to canceled.
// TODO: add casbin authorization — verify caller owns this refund
func (b *OrderHandler) CancelBuyerRefund(ctx restate.Context, params CancelBuyerRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate cancel refund", err)
	}

	// Fetch refund before cancelling to get order_id for notification
	type refundInfo struct {
		OrderID string `json:"order_id"`
	}
	ri, err := restate.Run(ctx, func(ctx restate.RunContext) (refundInfo, error) {
		r, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return refundInfo{}, err
		}
		if r.Status != orderdb.OrderStatusPending {
			return refundInfo{}, ordermodel.ErrRefundCannotBeUpdated.Terminal()
		}
		return refundInfo{OrderID: r.OrderID.String()}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch refund", err)
	}

	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().UpdateRefund(ctx, orderdb.UpdateRefundParams{
			ID:     params.RefundID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCancelled, Valid: true},
		})
		return err
	}); err != nil {
		return sharedmodel.WrapErr("db cancel refund", err)
	}

	// Notify seller (fire-and-forget, best effort)
	orderID, _ := uuid.Parse(ri.OrderID)
	order, err := b.GetBuyerOrder(ctx, orderID)
	if err == nil {
		meta, _ := json.Marshal(map[string]string{"order_id": ri.OrderID, "refund_id": params.RefundID.String()})
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: order.SellerID,
			Type:      accountmodel.NotiRefundCancelled,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Refund cancelled",
			Content: fmt.Sprintf(
				"Refund request for %s has been cancelled by the buyer.",
				ordermodel.SummarizeItems(order.Items),
			),
			Metadata: meta,
		})
	}

	return nil
}

// ConfirmSellerRefund marks a refund as confirmed by the vendor and transitions it to processing.
// TODO: add casbin authorization — verify caller is the order's seller
func (b *OrderHandler) ConfirmSellerRefund(
	ctx restate.Context,
	params ConfirmSellerRefundParams,
) (_ ordermodel.Refund, err error) {
	defer metrics.TrackHandler("order", "ConfirmSellerRefund", &err)()

	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate confirm refund", err)
	}

	refund, err := b.UpdateBuyerRefund(ctx, UpdateBuyerRefundParams{
		Account:       params.Account,
		RefundID:      params.RefundID,
		Status:        orderdb.OrderStatusProcessing,
		ConfirmedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("confirm refund", err)
	}

	// Notify customer: refund confirmed + check for auto-refund
	refundOrder, _ := b.GetBuyerOrder(ctx, refund.OrderID)
	refundSummary := "your order"
	if refundOrder.Items != nil {
		refundSummary = ordermodel.SummarizeItems(refundOrder.Items)
	}
	meta, _ := json.Marshal(map[string]string{"order_id": refund.OrderID.String(), "refund_id": refund.ID.String()})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: refund.AccountID,
		Type:      accountmodel.NotiRefundApproved,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Refund approved",
		Content:   fmt.Sprintf("Your refund for %s has been approved.", refundSummary),
		Metadata:  meta,
	})

	// Auto-refund for card payments: reuse hydrated order's payment data
	if refundOrder.Payment != nil && refundOrder.Payment.PaymentMethodID != nil {
		var data struct {
			ProviderChargeID string `json:"provider_charge_id"`
			Provider         string `json:"provider"`
		}
		_ = json.Unmarshal(refundOrder.Payment.Data, &data)
		if data.ProviderChargeID != "" {
			cardClient, err := b.getPaymentClientByProvider(data.Provider)
			if err == nil {
				// Partial refund: use refund.Amount if set, else full payment amount
				refundAmount := refundOrder.Payment.Amount
				if refund.Amount > 0 {
					refundAmount = refund.Amount
				}
				restate.RunVoid(ctx, func(ctx restate.RunContext) error {
					_, refundErr := cardClient.Refund(ctx, payment.RefundParams{
						ProviderChargeID: data.ProviderChargeID,
						Amount:           refundAmount,
					})
					if refundErr != nil {
						slog.Error("auto-refund failed",
							slog.String("refund_id", refund.ID.String()),
							slog.Any("error", refundErr),
						)
					}
					return nil
				})
			}
		}
	}

	return refund, nil
}
