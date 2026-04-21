// See: https://docs.giaohangtietkiem.vn/webhook
package orderbiz

import (
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
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
	// Terminal states
	orderdb.OrderTransportStatusDelivered: {},
	orderdb.OrderTransportStatusFailed: {
		orderdb.OrderTransportStatusInTransit:    true, // redelivery
		orderdb.OrderTransportStatusLabelCreated: true, // relabelled for redelivery
	},
	orderdb.OrderTransportStatusCancelled: {},
}

// UpdateTransportStatus updates a transport record's status and merges new event data
// into the JSONB data field. Enforces valid state transitions and fires notifications.
func (b *OrderHandler) UpdateTransportStatus(ctx restate.Context, params UpdateTransportStatusParams) error {
	var err error
	defer metrics.TrackHandler("order", "UpdateTransportStatus", &err)()

	if err = validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate update transport status", err)
	}

	// Resolve TransportID from TrackingID if needed (webhook sends provider label ID, not UUID)
	if params.TransportID == uuid.Nil && params.TrackingID != "" {
		t, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderTransport, error) {
			return b.storage.Querier().GetTransportByTrackingID(ctx, params.TrackingID)
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ordermodel.ErrOrderNotFound.Terminal()
			}
			return sharedmodel.WrapErr("get transport by tracking id", err)
		}
		params.TransportID = t.ID
	}
	if params.TransportID == uuid.Nil {
		return ordermodel.ErrOrderNotFound.Terminal()
	}

	type transportInfo struct {
		TransportID uuid.UUID `json:"transport_id"`
		OrderID     uuid.UUID `json:"order_id"`
		BuyerID     uuid.UUID `json:"buyer_id"`
		SellerID    uuid.UUID `json:"seller_id"`
		OldStatus   string    `json:"old_status"`
	}

	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (transportInfo, error) {
		var zero transportInfo
		row, err := b.storage.Querier().GetTransportWithOrder(ctx, params.TransportID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return zero, ordermodel.ErrOrderNotFound.Terminal()
			}
			return zero, sharedmodel.WrapErr("get transport with order", err)
		}

		// Validate state transition
		currentStatus := orderdb.OrderTransportStatusPending
		if row.Status.Valid {
			currentStatus = row.Status.OrderTransportStatus
		}

		allowed, ok := validTransitions[currentStatus]
		if !ok || !allowed[params.Status] {
			return zero, ordermodel.ErrTransportStatusInvalid.Fmt(currentStatus, params.Status).Terminal()
		}

		// Merge new event data into existing JSONB
		dataJSON, err := json.Marshal(params.Data)
		if err != nil {
			dataJSON = []byte("{}")
		}

		if err = b.storage.Querier().UpdateTransportStatus(ctx, orderdb.UpdateTransportStatusParams{
			ID: params.TransportID,
			Status: orderdb.NullOrderTransportStatus{
				OrderTransportStatus: params.Status,
				Valid:                true,
			},
			Column3: dataJSON,
		}); err != nil {
			return zero, sharedmodel.WrapErr("db update transport status", err)
		}

		metrics.TransportStatusUpdatesTotal.WithLabelValues(string(params.Status)).Inc()

		return zero, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("update transport status", err)
	}

	// Fire-and-forget notifications based on new status
	meta, _ := json.Marshal(map[string]string{
		"transport_id": fetched.TransportID.String(),
		"order_id":     fetched.OrderID.String(),
	})

	switch params.Status {
	case orderdb.OrderTransportStatusDelivered:
		if err != nil {
			slog.Warn("skip buyer notification: invalid buyer UUID", slog.String("buyer_id", fetched.BuyerID.String()), slog.Any("error", err))
		} else {
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: fetched.BuyerID,
				Type:      accountmodel.NotiTransportDelivered,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Đơn hàng đã được giao",
				Content:   "Đơn hàng của bạn đã được giao thành công.",
				Metadata:  meta,
			})
		}

	case orderdb.OrderTransportStatusFailed:
		if err != nil {
			slog.Warn("skip buyer notification: invalid buyer UUID", slog.String("buyer_id", fetched.BuyerID.String()), slog.Any("error", err))
		} else {
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: fetched.BuyerID,
				Type:      accountmodel.NotiTransportFailed,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Giao hàng thất bại",
				Content:   "Đơn hàng của bạn giao không thành công. Vui lòng liên hệ hỗ trợ.",
				Metadata:  meta,
			})
		}

		if err != nil {
			slog.Warn("skip seller notification: invalid seller UUID", slog.String("seller_id", fetched.SellerID.String()), slog.Any("error", err))
		} else {
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: fetched.SellerID,
				Type:      accountmodel.NotiSellerTransportFailed,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Giao hàng thất bại",
				Content:   "Đơn hàng đã giao không thành công.",
				Metadata:  meta,
			})

		}

	case orderdb.OrderTransportStatusCancelled:
		if err != nil {
			slog.Warn("skip buyer notification: invalid buyer UUID", slog.String("buyer_id", fetched.BuyerID.String()), slog.Any("error", err))
		} else {
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: fetched.BuyerID,
				Type:      accountmodel.NotiTransportCancelled,
				Channel:   accountmodel.ChannelInApp,
				Title:     "Đơn hàng đã bị hủy vận chuyển",
				Content:   "Đơn vận chuyển của bạn đã bị hủy.",
				Metadata:  meta,
			})
		}

		if err != nil {
			slog.Warn("skip seller notification: invalid seller UUID", slog.String("seller_id", fetched.SellerID.String()), slog.Any("error", err))
		} else {
			restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
				AccountID: fetched.SellerID,
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
