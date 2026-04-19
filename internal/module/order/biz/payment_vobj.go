package orderbiz

import (
	restate "github.com/restatedev/sdk-go"
)

// PaymentLock is a Restate Virtual Object keyed by payment ID (string).
// Uses VO state to track payment status — serial execution per key guarantees
// no TOCTOU races between ConfirmPayment, CancelUnpaidCheckout, and AutoCancel.
//
// State keys:
//   - "status": string — current payment status (empty = Pending)
type PaymentLock struct {
	handler *OrderHandler
}

func NewPaymentLock(handler *OrderHandler) *PaymentLock {
	return &PaymentLock{handler: handler}
}

func (v *PaymentLock) ServiceName() string { return "PaymentLock" }

// ConfirmPayment processes a payment webhook. Reads VO state to skip if already
// processed, then delegates to handler and updates state.
func (v *PaymentLock) ConfirmPayment(ctx restate.ObjectContext, params ConfirmPaymentParams) error {
	status, _ := restate.Get[string](ctx, "status")
	if status != "" && status != "Pending" {
		return nil // already confirmed or cancelled — idempotent skip
	}

	if err := v.handler.ConfirmPayment(ctx, params); err != nil {
		return err
	}

	restate.Set(ctx, "status", string(params.Status))
	return nil
}

// CancelUnpaidCheckout is called 15 min after checkout. Checks VO state to avoid
// cancelling a payment that was already confirmed by a webhook.
func (v *PaymentLock) CancelUnpaidCheckout(ctx restate.ObjectContext, paymentID int64) error {
	status, _ := restate.Get[string](ctx, "status")
	if status != "" && status != "Pending" {
		return nil // already confirmed or cancelled — skip
	}

	if err := v.handler.CancelUnpaidCheckout(ctx, paymentID); err != nil {
		return err
	}

	restate.Set(ctx, "status", "Cancelled")
	return nil
}

// AutoCancelPendingItems is called 48h after payment confirmation if seller hasn't
// confirmed. Checks VO state to ensure payment was actually confirmed.
func (v *PaymentLock) AutoCancelPendingItems(ctx restate.ObjectContext, paymentID int64) error {
	status, _ := restate.Get[string](ctx, "status")
	if status == "Cancelled" || status == "Failed" {
		return nil // payment was cancelled/failed — no items to cancel
	}

	return v.handler.AutoCancelPendingItems(ctx, paymentID)
}
