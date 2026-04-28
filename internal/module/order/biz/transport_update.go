// See: https://docs.giaohangtietkiem.vn/webhook
package orderbiz

import (
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

// validTransitions defines which OrderStatus transitions are allowed on the transport table.
// Delivered is mapped to OrderStatusSuccess; terminal states may not transition further.
// In-transit provider states (LabelCreated, InTransit, OutForDelivery) all map to
// OrderStatusProcessing before the caller sends them here.
var validTransitions = map[orderdb.OrderStatus]map[orderdb.OrderStatus]bool{
	orderdb.OrderStatusPending: {
		orderdb.OrderStatusProcessing: true, // LabelCreated / InTransit / OutForDelivery
		orderdb.OrderStatusFailed:     true,
		orderdb.OrderStatusCancelled:  true,
	},
	orderdb.OrderStatusProcessing: {
		orderdb.OrderStatusSuccess:   true, // Delivered
		orderdb.OrderStatusFailed:    true,
		orderdb.OrderStatusCancelled: true,
	},
	// Terminal states: Success (Delivered), Failed, Cancelled
	orderdb.OrderStatusSuccess:   {},
	orderdb.OrderStatusFailed:    {orderdb.OrderStatusProcessing: true}, // redelivery
	orderdb.OrderStatusCancelled: {},
}

// OnTransportResult updates a transport record's status and data field. When
// transport reaches OrderStatusSuccess (Delivered) it signals each affected
// order's PayoutWorkflow via OnRefundChanged so the in-flight workflow can
// re-evaluate its release-escrow timer; when transport fails or is cancelled
// it fires the corresponding buyer/seller notifications. Called by the
// transport-provider webhook bridge (transport/echo).
func (b *OrderHandler) OnTransportResult(ctx restate.Context, params UpdateTransportStatusParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate on transport result", err)
	}

	type transportInfo struct {
		TransportID int64  `json:"transport_id"`
		TrackingID  string `json:"tracking_id"`
	}

	// Step 1: Lookup by tracking ID, validate transition, update status.
	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (transportInfo, error) {
		var zero transportInfo

		tr, err := b.storage.Querier().GetTransportByTrackingID(ctx, json.RawMessage(`"`+params.TrackingID+`"`))
		if err != nil {
			return zero, ordermodel.ErrOrderNotFound.Terminal()
		}

		currentStatus := orderdb.OrderStatusPending
		if tr.Status.Valid {
			currentStatus = tr.Status.OrderStatus
		}

		allowed, ok := validTransitions[currentStatus]
		if !ok || !allowed[params.Status] {
			return zero, ordermodel.ErrTransportStatusInvalid.Fmt(currentStatus, params.Status).Terminal()
		}

		dataJSON := params.Data
		if len(dataJSON) == 0 {
			dataJSON = json.RawMessage("{}")
		}

		if _, err := b.storage.Querier().UpdateTransportStatusByID(ctx, orderdb.UpdateTransportStatusByIDParams{
			ID:     tr.ID,
			Status: orderdb.NullOrderStatus{OrderStatus: params.Status, Valid: true},
			Data:   dataJSON,
		}); err != nil {
			return zero, sharedmodel.WrapErr("db update transport status", err)
		}

		return transportInfo{
			TransportID: tr.ID,
			TrackingID:  params.TrackingID,
		}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("update transport status", err)
	}

	// Step 2: If Delivered (Success), fetch orders on this transport and signal
	// PayoutWorkflow so it can re-arm / wake up its escrow-release evaluation.
	if params.Status == orderdb.OrderStatusSuccess {
		orders, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderOrder, error) {
			return b.storage.Querier().ListOrdersByTransportID(ctx, fetched.TransportID)
		})
		if err != nil {
			return sharedmodel.WrapErr("list orders by transport", err)
		}
		for _, o := range orders {
			restate.WorkflowSend(ctx, "PayoutWorkflow", o.ID.String(), "OnRefundChanged").
				Send(struct{}{})
			// Notify buyer about delivery.
			meta, _ := json.Marshal(map[string]string{
				"tracking_id": fetched.TrackingID,
				"order_id":    o.ID.String(),
			})
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: o.BuyerID,
				Type:      accountmodel.NotiTransportDelivered,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Đơn hàng đã được giao",
				Content:   "Đơn hàng của bạn đã được giao thành công.",
				Metadata:  meta,
			})
		}
		return nil
	}

	// Step 3: Fire notifications for Failed / Cancelled statuses.
	// We need buyer + seller IDs from the order joined to this transport.
	type orderInfo struct {
		BuyerID  uuid.UUID `json:"buyer_id"`
		SellerID uuid.UUID `json:"seller_id"`
		OrderID  uuid.UUID `json:"order_id"`
		HasOrder bool      `json:"has_order"`
	}
	info, fetchErr := restate.Run(ctx, func(ctx restate.RunContext) (orderInfo, error) {
		r, err := b.storage.Querier().GetTransportWithOrder(ctx, fetched.TransportID)
		if err != nil {
			// Transport may not yet be linked to an order (early status updates).
			return orderInfo{HasOrder: false}, nil
		}
		return orderInfo{
			BuyerID:  r.OrderBuyerID,
			SellerID: r.OrderSellerID,
			OrderID:  r.OrderID,
			HasOrder: true,
		}, nil
	})
	if fetchErr != nil {
		slog.Warn("skip notifications: could not fetch transport order info",
			slog.String("tracking_id", params.TrackingID),
			slog.Any("error", fetchErr))
		return nil
	}
	if !info.HasOrder {
		return nil
	}

	meta, _ := json.Marshal(map[string]string{
		"tracking_id": params.TrackingID,
		"order_id":    info.OrderID.String(),
	})

	switch params.Status {
	case orderdb.OrderStatusFailed:
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: info.BuyerID,
			Type:      accountmodel.NotiTransportFailed,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Giao hàng thất bại",
			Content:   "Đơn hàng của bạn giao không thành công. Vui lòng liên hệ hỗ trợ.",
			Metadata:  meta,
		})
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: info.SellerID,
			Type:      accountmodel.NotiSellerTransportFailed,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Giao hàng thất bại",
			Content:   "Đơn hàng đã giao không thành công.",
			Metadata:  meta,
		})

	case orderdb.OrderStatusCancelled:
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: info.BuyerID,
			Type:      accountmodel.NotiTransportCancelled,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Đơn hàng đã bị hủy vận chuyển",
			Content:   "Đơn vận chuyển của bạn đã bị hủy.",
			Metadata:  meta,
		})
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: info.SellerID,
			Type:      accountmodel.NotiSellerTransportCancelled,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Đơn hàng đã bị hủy vận chuyển",
			Content:   "Đơn vận chuyển đã bị hủy.",
			Metadata:  meta,
		})
	}

	return nil
}
