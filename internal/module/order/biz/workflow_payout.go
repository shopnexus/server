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
	"shopnexus-server/internal/shared/saga"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type PayoutWorkflowHandler struct {
	base *OrderHandler
}

func NewPayoutWorkflowHandler(base *OrderHandler) *PayoutWorkflowHandler {
	return &PayoutWorkflowHandler{base: base}
}

func (h *PayoutWorkflowHandler) ServiceName() string { return "PayoutWorkflow" }

func (h *PayoutWorkflowHandler) Run(
	ctx restate.WorkflowContext,
	input PayoutInput,
) (out PayoutOutput, err error) {
	defer metrics.TrackHandler("payout_workflow", "Run", &err)()

	out.OrderID = input.OrderID

	saga := saga.New(ctx)
	defer func() {
		if restate.IsTerminalError(err) {
			saga.Compensate()
		}
	}()

	// Step 1: open the seller-payout session + Pending payout tx. UUIDs are
	// pre-allocated outside the Run so they're journaled by restate.UUID and
	// stable across replays — the INSERT becomes idempotent on PK conflict.
	sessionID := restate.UUID(ctx)
	payoutTxID := restate.UUID(ctx)

	// Compensator: mark the payout session + tx Failed if anything later in
	// the workflow terminally fails. Both queries guard on status='Pending'
	// so they no-op once the row reaches a final state — safe to retry.
	saga.Defer("mark_session_and_txs_failed", func(rctx restate.RunContext) error {
		return markSessionAndTxsFailed(
			rctx,
			h.base.storage.Querier(),
			sessionID,
			[]uuid.UUID{payoutTxID},
			"payout saga compensation",
		)
	})
	if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
		if _, sErr := h.base.storage.Querier().CreateDefaultPaymentSession(
			rctx,
			orderdb.CreateDefaultPaymentSessionParams{
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
			}); sErr != nil {
			return sharedmodel.WrapErr("db create payout session", sErr)
		}
		if _, txErr := h.base.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
			ID:            payoutTxID,
			SessionID:     sessionID,
			Status:        orderdb.OrderStatusPending,
			Note:          "seller payout (escrow)",
			Error:         null.String{},
			PaymentOption: null.String{},
			Data:          json.RawMessage("{}"),
			Amount:        input.PaidTotal,
			FromCurrency:  input.Currency,
			ToCurrency:    input.Currency,
			ExchangeRate:  mustNumericOne(),
			ReversesID:    uuid.NullUUID{},
			DateSettled:   null.Time{},
			DateExpired:   null.Time{},
		}); txErr != nil {
			return sharedmodel.WrapErr("db create payout tx", txErr)
		}
		return nil
	}); err != nil {
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
			// and notify the seller. No wallet credit happens. Saga stays
			// armed: if MarkPaymentSessionCancelled fails terminal, the
			// compensator marks the still-Pending row Failed (better than
			// stuck Pending forever).
			if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				if _, e := h.base.storage.Querier().MarkPaymentSessionCancelled(rctx, sessionID); e != nil {
					return sharedmodel.WrapErr("mark payout session cancelled", e)
				}
				return nil
			}); err != nil {
				return out, err
			}
			notifySellerPayoutCancelled(ctx, input.SellerID, input.OrderID)
			out.Outcome = "refunded"
			return out, nil

		case !snap.HasActiveRefund && !time.Now().Before(deadline):
			// Escrow window has elapsed with no active refund — release.
			// Each step is wrapped in restate.RunVoid so replays after a
			// crash use the journaled result instead of re-executing the
			// Pending-guarded UPDATE (which would fail with ErrNoRows on a
			// row already marked Success).
			if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				if _, e := h.base.storage.Querier().MarkPaymentSessionSuccess(rctx, orderdb.MarkPaymentSessionSuccessParams{
					ID: sessionID,
				}); e != nil {
					return sharedmodel.WrapErr("mark payout session success", e)
				}
				if _, e := h.base.storage.Querier().MarkTransactionSuccess(rctx, orderdb.MarkTransactionSuccessParams{
					ID: payoutTxID,
				}); e != nil {
					return sharedmodel.WrapErr("mark payout tx success", e)
				}
				return nil
			}); err != nil {
				return out, sharedmodel.WrapErr("mark payout success", err)
			}
			// Saga stays armed (no Clear). The Pending-guarded compensator
			// no-ops on the now-Success rows, so a terminal failure of the
			// wallet credit below does NOT auto-revert the marks — operator
			// intervention is required for that gap.
			if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				return h.base.walletCredit(rctx, WalletCreditParams{
					AccountID: input.SellerID,
					Amount:    input.PaidTotal,
					Type:      "Payout",
					Reference: fmt.Sprintf("payout-session:%s", sessionID),
					Note:      "escrow released",
				})
			}); err != nil {
				return out, sharedmodel.WrapErr("seller wallet credit", err)
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

		futs := []restate.Future{signal}
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
