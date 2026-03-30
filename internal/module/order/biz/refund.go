package orderbiz

import (
	"encoding/json"
	"fmt"
	"log/slog"

	restate "github.com/restatedev/sdk-go"

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
func (b *OrderHandler) ListBuyerRefunds(ctx restate.Context, params ListBuyerRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list refunds", err)
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

// CreateBuyerRefund creates a new refund request for an order and tracks refund analytics.
func (b *OrderHandler) CreateBuyerRefund(ctx restate.Context, params CreateBuyerRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate create refund", err)
	}

	if params.Method == orderdb.OrderRefundMethodPickUp && !params.Address.Valid {
		return zero, ordermodel.ErrRefundAddressRequired.Terminal()
	}

	// Create refund + update resources in one durable step
	refund, err := restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Refund, error) {
		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: params.OrderID, Valid: true})
		if err != nil {
			return zero, sharedmodel.WrapErr("db get order", err)
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
			return zero, sharedmodel.WrapErr("db create refund", err)
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
			return zero, sharedmodel.WrapErr("update refund resources", err)
		}

		m := dbToRefund(dbRefund)
		m.Resources = resources
		return m, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create refund", err)
	}

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
func (b *OrderHandler) UpdateBuyerRefund(ctx restate.Context, params UpdateBuyerRefundParams) (ordermodel.Refund, error) {
	var zero ordermodel.Refund

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate update refund", err)
	}

	return restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Refund, error) {
		refund, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return zero, sharedmodel.WrapErr("db get refund", err)
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
			return zero, sharedmodel.WrapErr("db update refund", err)
		}

		resources, err := b.common.UpdateResources(ctx, commonbiz.UpdateResourcesParams{
			Account:         params.Account,
			RefType:         commondb.CommonResourceRefTypeRefund,
			RefID:           refund.ID,
			ResourceIDs:     params.ResourceIDs,
			DeleteResources: true,
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("update refund resources", err)
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

// CancelBuyerRefund cancels a refund request by setting its status to canceled.
func (b *OrderHandler) CancelBuyerRefund(ctx restate.Context, params CancelBuyerRefundParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate cancel refund", err)
	}

	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		if _, err := b.storage.Querier().UpdateRefund(ctx, orderdb.UpdateRefundParams{
			ID:     params.RefundID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return sharedmodel.WrapErr(fmt.Sprintf("db cancel refund %s", params.RefundID), err)
		}
		return nil
	}); err != nil {
		return err
	}

	// Notify seller: refund cancelled (fire-and-forget, best effort)
	refundData, err := restate.Run(ctx, func(ctx restate.RunContext) (ordermodel.Refund, error) {
		r, err := b.storage.Querier().GetRefund(ctx, uuid.NullUUID{UUID: params.RefundID, Valid: true})
		if err != nil {
			return ordermodel.Refund{}, err
		}
		return dbToRefund(r), nil
	})
	if err == nil {
		order, err := b.GetBuyerOrder(ctx, refundData.OrderID)
		if err == nil {
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: order.SellerID,
				Type:      accountmodel.NotiRefundCancelled,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Refund cancelled",
				Content:   fmt.Sprintf("Refund request for %s has been cancelled by the buyer.", ordermodel.SummarizeItems(order.Items)),
				Metadata:  json.RawMessage(fmt.Sprintf(`{"order_id":"%s","refund_id":"%s"}`, refundData.OrderID, refundData.ID)),
			})
		}
	}

	return nil
}

// ConfirmSellerRefund marks a refund as confirmed by the vendor and transitions it to processing.
func (b *OrderHandler) ConfirmSellerRefund(ctx restate.Context, params ConfirmSellerRefundParams) (ordermodel.Refund, error) {
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

	// Notify customer: refund confirmed
	refundOrder, _ := b.GetBuyerOrder(ctx, refund.OrderID)
	refundSummary := "your order"
	if refundOrder.Items != nil {
		refundSummary = ordermodel.SummarizeItems(refundOrder.Items)
	}
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: refund.AccountID,
		Type:      accountmodel.NotiRefundApproved,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Refund approved",
		Content:   fmt.Sprintf("Your refund for %s has been approved.", refundSummary),
		Metadata:  json.RawMessage(fmt.Sprintf(`{"order_id":"%s","refund_id":"%s"}`, refund.OrderID, refund.ID)),
	})

	// Auto-refund for card payments: look up the order's payment record and
	// check whether it was charged via a saved payment method. If so, issue
	// a provider-level refund automatically.
	type chargeInfo struct {
		HasCharge bool   `json:"has_charge"`
		Provider  string `json:"provider"`
		ChargeID  string `json:"charge_id"`
		Amount    int64  `json:"amount"`
	}
	ci, _ := restate.Run(ctx, func(ctx restate.RunContext) (chargeInfo, error) {
		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: refund.OrderID, Valid: true})
		if err != nil || !order.PaymentID.Valid {
			return chargeInfo{}, nil
		}
		payments, err := b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
			ID: []int64{order.PaymentID.Int64},
		})
		if err != nil || len(payments) == 0 || !payments[0].PaymentMethodID.Valid {
			return chargeInfo{}, nil
		}
		p := payments[0]
		var data struct {
			ProviderChargeID string `json:"provider_charge_id"`
			Provider         string `json:"provider"`
		}
		_ = json.Unmarshal(p.Data, &data)
		if data.ProviderChargeID == "" {
			return chargeInfo{}, nil
		}
		return chargeInfo{
			HasCharge: true,
			Provider:  data.Provider,
			ChargeID:  data.ProviderChargeID,
			Amount:    p.Amount,
		}, nil
	})

	if ci.HasCharge {
		cardClient, err := b.getPaymentClientByProvider(ci.Provider)
		if err == nil {
			restate.RunVoid(ctx, func(ctx restate.RunContext) error {
				_, refundErr := cardClient.Refund(ctx, payment.RefundParams{
					ProviderChargeID: ci.ChargeID,
					Amount:           sharedmodel.Concurrency(ci.Amount),
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

	return refund, nil
}
