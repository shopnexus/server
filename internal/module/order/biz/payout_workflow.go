package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

// PayoutWorkflowHandler is the long-running watcher that holds buyer funds in
// escrow after a confirm settles. Unlike Checkout / Confirm, there is no saga
// here: the workflow either releases the payout (escrow window expired with no
// approved refund) or cancels it (a refund settled in Success first). It loops
// each iteration, reloading the refund snapshot, and races a per-iteration
// signal promise against the remaining escrow timer.
type PayoutWorkflowHandler struct {
	base *OrderHandler
}

// NewPayoutWorkflowHandler wraps an OrderHandler so the workflow can reuse
// every helper (storage, account proxy for wallet credits, MarkTxSuccess,
// etc.). The workflow ID is the order UUID — set by the upstream
// ConfirmWorkflow when it hands off via WorkflowSend.
func NewPayoutWorkflowHandler(base *OrderHandler) *PayoutWorkflowHandler {
	return &PayoutWorkflowHandler{base: base}
}

// ServiceName overrides the embedded OrderHandler's name so restate.Reflect
// registers this struct under "PayoutWorkflow".
func (h *PayoutWorkflowHandler) ServiceName() string { return "PayoutWorkflow" }

// Run is the workflow body. It is invoked once per confirmed order and
// (a) inserts the seller-payout session + a Pending payout transaction, then
// (b) loops: reload refund snapshot, decide release / refund / wait, and on
// "wait" race the next signal promise against the escrow deadline. Iteration
// counter lives in Restate K/V state ("refund_iter") so OnRefundChanged can
// resolve the exact promise the loop is currently blocked on without needing
// to know the current iteration ahead of time.
func (h *PayoutWorkflowHandler) Run(
	ctx restate.WorkflowContext,
	input PayoutInput,
) (out PayoutOutput, err error) {
	defer metrics.TrackHandler("payout_workflow", "Run", &err)()

	out.OrderID = input.OrderID

	// Step 1: open the seller-payout session + Pending payout tx. The session
	// id is generated inside Run so its journaled value is stable across
	// replays. payment_session.id has no DB default — the workflow is the
	// source of truth for the UUID.
	type initResult struct {
		SessionID uuid.UUID `json:"session_id"`
		TxID      int64     `json:"tx_id"`
	}
	init, err := restate.Run(ctx, func(rctx restate.RunContext) (initResult, error) {
		var res initResult
		sessionID := uuid.New()
		_, sErr := h.base.storage.Querier().CreateDefaultPaymentSession(rctx, orderdb.CreateDefaultPaymentSessionParams{
			ID:          sessionID,
			Kind:        SessionKindSellerPayout,
			Status:      orderdb.OrderStatusPending,
			FromID:      uuid.NullUUID{},
			ToID:        uuid.NullUUID{UUID: input.SellerID, Valid: true},
			Note:        "seller payout (escrow)",
			Currency:    input.Currency,
			TotalAmount: input.PaidTotal,
			Data:        json.RawMessage("{}"),
			DatePaid:    null.Time{},
			DateExpired: time.Now().Add(escrowWindow),
		})
		if sErr != nil {
			return res, sharedmodel.WrapErr("db create payout session", sErr)
		}
		tx, txErr := h.base.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
			SessionID:     sessionID,
			Status:        orderdb.OrderStatusPending,
			Note:          "seller payout (escrow)",
			Error:         null.String{},
			PaymentOption: null.String{},
			WalletID:      uuid.NullUUID{},
			Data:          json.RawMessage("{}"),
			Amount:        input.PaidTotal,
			FromCurrency:  input.Currency,
			ToCurrency:    input.Currency,
			ExchangeRate:  mustNumericOne(),
			ReversesID:    null.Int{},
			DateSettled:   null.Time{},
			DateExpired:   null.Time{},
		})
		if txErr != nil {
			return res, sharedmodel.WrapErr("db create payout tx", txErr)
		}
		res.SessionID = sessionID
		res.TxID = tx.ID
		return res, nil
	})
	if err != nil {
		return out, sharedmodel.WrapErr("init payout session", err)
	}

	deadline := time.Now().Add(escrowWindow)
	iter := 0

	for {
		// Reload the refund snapshot each iteration. It's tiny and journaling
		// it lets the next decision branch off a deterministic value.
		snap, snapErr := restate.Run(ctx, func(rctx restate.RunContext) (RefundSnapshot, error) {
			row, e := h.base.storage.Querier().GetRefundSnapshotByOrder(rctx, input.OrderID)
			if e != nil {
				return RefundSnapshot{}, e
			}
			return RefundSnapshot{
				HasActiveRefund:    row.HasActiveRefund,
				LastRefundApproved: row.LastRefundApproved,
			}, nil
		})
		if snapErr != nil {
			return out, sharedmodel.WrapErr("reload refund snapshot", snapErr)
		}

		switch {
		case snap.LastRefundApproved:
			// A refund has settled — cancel the pending payout session/tx
			// and notify the seller. No wallet credit happens.
			if cErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				if _, e := h.base.storage.Querier().MarkPaymentSessionCancelled(rctx, init.SessionID); e != nil {
					return sharedmodel.WrapErr("mark payout session cancelled", e)
				}
				return nil
			}); cErr != nil {
				return out, cErr
			}
			notifySellerPayoutCancelled(ctx, input.SellerID, input.OrderID)
			out.Outcome = "refunded"
			return out, nil

		case !snap.HasActiveRefund && !time.Now().Before(deadline):
			// Escrow window has elapsed with no active refund — release.
			if rErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				return h.base.releasePayout(rctx, releasePayoutParams{
					SessionID: init.SessionID,
					TxID:      init.TxID,
					Input:     input,
				})
			}); rErr != nil {
				return out, sharedmodel.WrapErr("release payout", rErr)
			}
			notifySellerPayoutReleased(ctx, input.SellerID, input.OrderID)
			out.Outcome = "released"
			return out, nil
		}

		// Wait for either a refund-state change (signalled by OnRefundChanged
		// against the iter-suffixed promise key) or the escrow deadline.
		iter++
		restate.Set(ctx, "refund_iter", iter)
		signal := restate.Promise[any](ctx, fmt.Sprintf("refund_changed_%d", iter))

		futs := []restate.Selectable{signal}
		if remaining := time.Until(deadline); remaining > 0 {
			futs = append(futs, restate.After(ctx, remaining))
		}
		// If both the signal hasn't fired and the deadline already passed,
		// loop immediately — the switch above will handle the release path.
		if len(futs) > 1 {
			_, _ = restate.WaitFirst(ctx, futs...)
		} else {
			// Only the signal is left to wait on. This branch is hit when the
			// deadline has passed but a refund is still active, so we wait
			// indefinitely for the refund to settle one way or the other.
			_, _ = signal.Result()
		}
	}
}

// OnRefundChanged is the shared-mode entry point invoked by the refund module
// every time a refund row for this order transitions state. It resolves the
// promise the Run loop is currently blocked on, identified by the iteration
// counter we persist in K/V state.
func (h *PayoutWorkflowHandler) OnRefundChanged(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) error {
	iter, err := restate.Get[int](ctx, "refund_iter")
	if err != nil {
		return sharedmodel.WrapErr("read refund_iter state", err)
	}
	// iter == 0 means Run hasn't started its wait loop yet — the next
	// iteration's reload will pick up the change anyway, so no-op.
	if iter == 0 {
		return nil
	}
	return restate.Promise[any](ctx, fmt.Sprintf("refund_changed_%d", iter)).Resolve(nil)
}

// releasePayoutParams bundles the inputs releasePayout needs so the helper
// can be called from inside a single restate.Run / RunVoid closure.
type releasePayoutParams struct {
	SessionID uuid.UUID
	TxID      int64
	Input     PayoutInput
}

// releasePayout marks the payout session + tx Success and credits the
// seller's wallet. Lives on *OrderHandler so other flows (admin force-release,
// timeout sweep) can reuse it. The cross-module WalletCredit goes through the
// account proxy — its interface signature takes context.Context, which
// restate.RunContext satisfies, so we can run the whole sequence inside one
// Run for atomic-on-replay semantics.
func (h *OrderHandler) releasePayout(rctx restate.RunContext, p releasePayoutParams) error {
	if _, e := h.storage.Querier().MarkPaymentSessionSuccess(rctx, orderdb.MarkPaymentSessionSuccessParams{
		ID: p.SessionID,
	}); e != nil {
		return sharedmodel.WrapErr("mark payout session success", e)
	}
	if _, e := h.storage.Querier().MarkTransactionSuccess(rctx, orderdb.MarkTransactionSuccessParams{
		ID: p.TxID,
	}); e != nil {
		return sharedmodel.WrapErr("mark payout tx success", e)
	}
	if e := h.account.WalletCredit(rctx, accountbiz.WalletCreditParams{
		AccountID: p.Input.SellerID,
		Amount:    p.Input.PaidTotal,
		Type:      "Payout",
		Reference: fmt.Sprintf("payout-session:%s", p.SessionID),
		Note:      "escrow released",
	}); e != nil {
		return sharedmodel.WrapErr("seller wallet credit", e)
	}
	return nil
}

func notifySellerPayoutReleased(ctx restate.WorkflowContext, sellerID, orderID uuid.UUID) {
	meta, _ := json.Marshal(map[string]string{"order_id": orderID.String()})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: sellerID,
		Type:      accountmodel.NotiPayoutReleased,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Payout released",
		Content:   "Your escrow payout has been released to your wallet.",
		Metadata:  meta,
	})
}

func notifySellerPayoutCancelled(ctx restate.WorkflowContext, sellerID, orderID uuid.UUID) {
	meta, _ := json.Marshal(map[string]string{"order_id": orderID.String()})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: sellerID,
		Type:      accountmodel.NotiPayoutCancelled,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Payout cancelled",
		Content:   "An approved refund has cancelled the escrow payout for this order.",
		Metadata:  meta,
	})
}
