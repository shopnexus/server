package orderbiz

import (
	ordermodel "shopnexus-server/internal/module/order/model"

	restate "github.com/restatedev/sdk-go"
)

// RefundLock is a Restate Virtual Object keyed by refund ID (string).
// Uses VO state to track refund status — serial execution per key guarantees
// no TOCTOU races between buyer update/cancel and seller confirm.
//
// State keys:
//   - "status": string — current refund status (empty = Pending)
type RefundLock struct {
	handler *OrderHandler
}

func NewRefundLock(handler *OrderHandler) *RefundLock {
	return &RefundLock{handler: handler}
}

func (v *RefundLock) ServiceName() string { return "RefundLock" }

// UpdateBuyerRefund updates a pending refund. Only allowed when VO status is Pending.
func (v *RefundLock) UpdateBuyerRefund(ctx restate.ObjectContext, params UpdateBuyerRefundParams) (ordermodel.Refund, error) {
	status, _ := restate.Get[string](ctx, "status")
	if status != "" && status != "Pending" {
		return ordermodel.Refund{}, ordermodel.ErrRefundCannotBeUpdated.Terminal()
	}

	return v.handler.UpdateBuyerRefund(ctx, params)
}

// CancelBuyerRefund cancels a pending refund. Only allowed when VO status is Pending.
func (v *RefundLock) CancelBuyerRefund(ctx restate.ObjectContext, params CancelBuyerRefundParams) error {
	status, _ := restate.Get[string](ctx, "status")
	if status != "" && status != "Pending" {
		return ordermodel.ErrRefundCannotBeUpdated.Terminal()
	}

	if err := v.handler.CancelBuyerRefund(ctx, params); err != nil {
		return err
	}

	restate.Set(ctx, "status", "Cancelled")
	return nil
}

// ConfirmSellerRefund confirms a refund and triggers auto-refund. Only allowed when
// VO status is Pending. Sets status to Processing after success.
func (v *RefundLock) ConfirmSellerRefund(ctx restate.ObjectContext, params ConfirmSellerRefundParams) (ordermodel.Refund, error) {
	status, _ := restate.Get[string](ctx, "status")
	if status != "" && status != "Pending" {
		return ordermodel.Refund{}, ordermodel.ErrRefundCannotBeUpdated.Terminal()
	}

	result, err := v.handler.ConfirmSellerRefund(ctx, params)
	if err != nil {
		return result, err
	}

	restate.Set(ctx, "status", "Processing")
	return result, nil
}
