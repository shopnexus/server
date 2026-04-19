package orderbiz

import (
	"strconv"

	"github.com/google/uuid"
	"github.com/samber/lo"
	restate "github.com/restatedev/sdk-go"
)

// PaymentLock is a Restate Virtual Object keyed by payment ID (string).
// Manages payment lifecycle state via restate.Get/Set — serial execution per key
// eliminates all TOCTOU races between webhook confirms, timeout cancels, and buyer actions.
//
// State keys:
//   - "status": string — Pending | Success | Failed | Cancelled
//   - "items":  []PaymentLockItem — items linked to this payment (for state tracking)
type PaymentLock struct {
	handler *OrderHandler
}

// PaymentLockItem holds minimal item info stored in VO state at Init time.
type PaymentLockItem struct {
	ID    int64     `json:"id"`
	SkuID uuid.UUID `json:"sku_id"`
}

func NewPaymentLock(handler *OrderHandler) *PaymentLock {
	return &PaymentLock{handler: handler}
}

func (v *PaymentLock) ServiceName() string { return "PaymentLock" }

// Init stores initial payment state. Called by checkout after creating payment + items.
func (v *PaymentLock) Init(ctx restate.ObjectContext, items []PaymentLockItem) error {
	restate.Set(ctx, "status", "Pending")
	restate.Set(ctx, "items", items)
	return nil
}

// ConfirmPayment processes a payment webhook. Checks VO status — if not Pending,
// skips (idempotent). Delegates to handler for DB work, then updates VO state.
func (v *PaymentLock) ConfirmPayment(ctx restate.ObjectContext, params ConfirmPaymentParams) error {
	status, _ := restate.Get[string](ctx, "status")
	if status != "" && status != "Pending" {
		return nil // already processed
	}

	if err := v.handler.ConfirmPayment(ctx, params); err != nil {
		return err
	}

	restate.Set(ctx, "status", string(params.Status))
	return nil
}

// CancelUnpaidCheckout fires 15 min after checkout. Checks VO status —
// if payment was already confirmed, skips. Delegates to handler for full cancel logic.
func (v *PaymentLock) CancelUnpaidCheckout(ctx restate.ObjectContext, paymentID int64) error {
	status, _ := restate.Get[string](ctx, "status")
	if status != "" && status != "Pending" {
		return nil // already confirmed or cancelled
	}

	if err := v.handler.CancelUnpaidCheckout(ctx, paymentID); err != nil {
		return err
	}

	restate.Set(ctx, "status", "Cancelled")
	restate.Clear(ctx, "items")
	return nil
}

// AutoCancelPendingItems fires 48h after payment success. Skips if payment was voided.
func (v *PaymentLock) AutoCancelPendingItems(ctx restate.ObjectContext, paymentID int64) error {
	status, _ := restate.Get[string](ctx, "status")
	if status == "Cancelled" || status == "Failed" {
		return nil // payment voided
	}

	return v.handler.AutoCancelPendingItems(ctx, paymentID)
}

// CancelBuyerPending cancels a single pending item. Checks payment is active,
// delegates to handler, then updates VO item list.
func (v *PaymentLock) CancelBuyerPending(ctx restate.ObjectContext, params CancelBuyerPendingParams) error {
	status, _ := restate.Get[string](ctx, "status")
	if status == "Cancelled" || status == "Failed" {
		return nil
	}

	if err := v.handler.CancelBuyerPending(ctx, params); err != nil {
		return err
	}

	// Remove cancelled item from VO state
	items, _ := restate.Get[[]PaymentLockItem](ctx, "items")
	items = lo.Filter(items, func(item PaymentLockItem, _ int) bool {
		return item.ID != params.ItemID
	})
	if len(items) == 0 {
		restate.Set(ctx, "status", "Cancelled")
		restate.Clear(ctx, "items")
	} else {
		restate.Set(ctx, "items", items)
	}

	return nil
}

// PaymentKeyFromID converts an int64 payment ID to the VO key string.
func PaymentKeyFromID(paymentID int64) string {
	return strconv.FormatInt(paymentID, 10)
}
