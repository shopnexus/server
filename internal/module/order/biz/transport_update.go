// See: https://docs.giaohangtietkiem.vn/webhook
package orderbiz

import (
	"encoding/json"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

// validTransitions defines which transitions are allowed.
// Terminal states (Delivered, Failed, Cancelled) may not transition further.
// Any status can move to Failed or Cancelled (exception path).
var validTransitions = map[orderdb.OrderTransportStatus]map[orderdb.OrderTransportStatus]bool{
	orderdb.OrderTransportStatusPending: {
		orderdb.OrderTransportStatusLabelCreated: true,
		orderdb.OrderTransportStatusFailed:       true,
		orderdb.OrderTransportStatusCancelled:    true,
	},
	orderdb.OrderTransportStatusLabelCreated: {
		orderdb.OrderTransportStatusInTransit: true,
		orderdb.OrderTransportStatusFailed:    true,
		orderdb.OrderTransportStatusCancelled: true,
	},
	orderdb.OrderTransportStatusInTransit: {
		orderdb.OrderTransportStatusOutForDelivery: true,
		orderdb.OrderTransportStatusFailed:         true,
		orderdb.OrderTransportStatusCancelled:      true,
	},
	orderdb.OrderTransportStatusOutForDelivery: {
		orderdb.OrderTransportStatusDelivered: true,
		orderdb.OrderTransportStatusFailed:    true,
		orderdb.OrderTransportStatusCancelled: true,
	},
	// Terminal states — no further transitions allowed
	orderdb.OrderTransportStatusDelivered: {},
	orderdb.OrderTransportStatusFailed:    {},
	orderdb.OrderTransportStatusCancelled: {},
}

// transportOrderInfo is a JSON-safe struct for restate.Run journal serialization.
type transportOrderInfo struct {
	TransportID string `json:"transport_id"`
	OrderID     string `json:"order_id"`
	BuyerID     string `json:"buyer_id"`
	SellerID    string `json:"seller_id"`
	OldStatus   string `json:"old_status"`
}

// UpdateTransportStatus updates a transport record's status and merges new event data
// into the JSONB data field. Enforces valid state transitions and fires notifications.
func (b *OrderHandler) UpdateTransportStatus(ctx restate.Context, params UpdateTransportStatusParams) (err error) {
	defer metrics.TrackHandler("order", "UpdateTransportStatus", &err)()

	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate update transport status", err)
	}

	// Resolve TransportID from TrackingID if needed (webhook sends provider label ID, not UUID)
	if params.TransportID == uuid.Nil && params.TrackingID != "" {
		t, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderTransport, error) {
			return b.storage.Querier().GetTransportByTrackingID(ctx, params.TrackingID)
		})
		if err != nil {
			return ordermodel.ErrOrderNotFound.Terminal()
		}
		params.TransportID = t.ID
	}
	if params.TransportID == uuid.Nil {
		return ordermodel.ErrOrderNotFound.Terminal()
	}

	type fetchResult struct {
		Info transportOrderInfo `json:"info"`
	}

	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (fetchResult, error) {
		row, err := b.storage.Querier().GetTransportWithOrder(ctx, params.TransportID)
		if err != nil {
			return fetchResult{}, ordermodel.ErrOrderNotFound.Terminal()
		}

		// Validate state transition
		currentStatus := orderdb.OrderTransportStatusPending
		if row.Status.Valid {
			currentStatus = row.Status.OrderTransportStatus
		}

		allowed, ok := validTransitions[currentStatus]
		if !ok || !allowed[params.Status] {
			return fetchResult{}, sharedmodel.NewError(409,
				fmt.Sprintf("cannot transition transport from %s to %s", currentStatus, params.Status)).Terminal()
		}

		// Merge new event data into existing JSONB
		dataJSON, err := json.Marshal(params.Data)
		if err != nil {
			dataJSON = []byte("{}")
		}

		if err := b.storage.Querier().UpdateTransportStatus(ctx, orderdb.UpdateTransportStatusParams{
			ID: params.TransportID,
			Status: orderdb.NullOrderTransportStatus{
				OrderTransportStatus: params.Status,
				Valid:                true,
			},
			Column3: dataJSON,
		}); err != nil {
			return fetchResult{}, sharedmodel.WrapErr("db update transport status", err)
		}

		metrics.TransportStatusUpdatesTotal.WithLabelValues(string(params.Status)).Inc()

		info := transportOrderInfo{
			TransportID: row.ID.String(),
			OldStatus:   string(currentStatus),
		}
		if row.OrderID.Valid {
			info.OrderID = row.OrderID.UUID.String()
		}
		if row.OrderBuyerID.Valid {
			info.BuyerID = row.OrderBuyerID.UUID.String()
		}
		if row.OrderSellerID.Valid {
			info.SellerID = row.OrderSellerID.UUID.String()
		}

		return fetchResult{Info: info}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("update transport status", err)
	}

	info := fetched.Info

	// Fire-and-forget notifications based on new status
	meta, _ := json.Marshal(map[string]string{
		"transport_id": info.TransportID,
		"order_id":     info.OrderID,
	})

	switch params.Status {
	case orderdb.OrderTransportStatusDelivered:
		if info.BuyerID != "" {
			buyerID, _ := uuid.Parse(info.BuyerID)
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: buyerID,
				Type:      accountmodel.NotiTransportDelivered,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Đơn hàng đã được giao",
				Content:   "Đơn hàng của bạn đã được giao thành công.",
				Metadata:  meta,
			})
		}

	case orderdb.OrderTransportStatusFailed:
		if info.BuyerID != "" {
			buyerID, _ := uuid.Parse(info.BuyerID)
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: buyerID,
				Type:      accountmodel.NotiTransportFailed,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Giao hàng thất bại",
				Content:   "Đơn hàng của bạn giao không thành công. Vui lòng liên hệ hỗ trợ.",
				Metadata:  meta,
			})
		}
		if info.SellerID != "" {
			sellerID, _ := uuid.Parse(info.SellerID)
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: sellerID,
				Type:      accountmodel.NotiSellerTransportFailed,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Giao hàng thất bại",
				Content:   "Đơn hàng đã giao không thành công.",
				Metadata:  meta,
			})
		}

	case orderdb.OrderTransportStatusCancelled:
		if info.BuyerID != "" {
			buyerID, _ := uuid.Parse(info.BuyerID)
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: buyerID,
				Type:      accountmodel.NotiTransportCancelled,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Đơn hàng đã bị hủy vận chuyển",
				Content:   "Đơn vận chuyển của bạn đã bị hủy.",
				Metadata:  meta,
			})
		}
		if info.SellerID != "" {
			sellerID, _ := uuid.Parse(info.SellerID)
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: sellerID,
				Type:      accountmodel.NotiSellerTransportCancelled,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Đơn hàng đã bị hủy vận chuyển",
				Content:   "Đơn vận chuyển đã bị hủy.",
				Metadata:  meta,
			})
		}
	}

	return nil
}
