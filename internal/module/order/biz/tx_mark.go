package orderbiz

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

// markTxSuccess transitions a Pending transaction to Success and, if this was the
// last Pending tx in its session (and no leg failed), auto-promotes the session
// to Success. Internal — invoked by OnPaymentResult and the workflow handlers
// after wallet debit/credit acknowledgements.
func (b *OrderHandler) markTxSuccess(ctx restate.Context, params markTxSuccessParams) error {
	dateAt := params.DateAt
	if dateAt.IsZero() {
		dateAt = time.Now()
	}
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		tx, err := b.storage.Querier().MarkTransactionSuccess(ctx, orderdb.MarkTransactionSuccessParams{
			ID:          params.TxID,
			DateSettled: dateAt,
		})
		if err != nil {
			return sharedmodel.WrapErr("mark tx success", err)
		}

		// Auto-promote session: if all sibling txs are non-Pending and none Failed,
		// flip session to Success. Idempotent — MarkPaymentSessionSuccess no-ops
		// (ErrNoRows) if the session already left Pending.
		siblings, err := b.storage.Querier().ListTransactionsBySession(ctx, tx.SessionID)
		if err != nil {
			return sharedmodel.WrapErr("list session siblings", err)
		}
		allSettled := true
		for _, s := range siblings {
			if s.Status == orderdb.OrderStatusPending {
				allSettled = false
				break
			}
			if s.Status == orderdb.OrderStatusFailed {
				return nil // a leg failed — leave session for saga / refund flow to mark Failed
			}
		}
		if !allSettled {
			return nil
		}
		if _, err := b.storage.Querier().MarkPaymentSessionSuccess(ctx, orderdb.MarkPaymentSessionSuccessParams{
			ID:       tx.SessionID,
			DatePaid: dateAt,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return sharedmodel.WrapErr("auto-promote session success", err)
		}
		return nil
	})
}

// markTxFailed transitions a Pending transaction to Failed.
// Internal — invoked by OnPaymentResult on gateway decline/error.
func (b *OrderHandler) markTxFailed(ctx restate.Context, params markTxFailedParams) error {
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().MarkTransactionFailed(ctx, orderdb.MarkTransactionFailedParams{
			ID:    params.TxID,
			Error: null.NewString(params.Reason, params.Reason != ""),
		})
		return err
	})
}

// OnPaymentResult is the unified entry point for gateway IPN webhooks.
// It resolves the gateway-leg tx (from params or by walking the session),
// updates DB tx + auto-promotes the session via MarkTxSuccess/MarkTxFailed,
// then signals the owning workflow's `payment_event` promise via
// WorkflowSend. Sessions outside the workflow scheme (e.g. payout) silently
// no-op on the workflow signal.
func (b *OrderHandler) OnPaymentResult(ctx restate.Context, params OnPaymentResultParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate on payment result", err)
	}

	// Step 1: load session + resolve TxID if the webhook didn't supply one.
	session, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(rctx, uuid.NullUUID{UUID: params.SessionID, Valid: true})
	})
	if err != nil {
		return sharedmodel.WrapErr("get session", err)
	}

	txID := params.TxID
	if txID == 0 {
		resolved, err := restate.Run(ctx, func(rctx restate.RunContext) (int64, error) {
			txs, err := b.storage.Querier().ListTransactionsBySession(rctx, params.SessionID)
			if err != nil {
				return 0, err
			}
			// Pick the Pending gateway-leg tx (no WalletID set).
			for _, t := range txs {
				if t.Status == orderdb.OrderStatusPending && !t.WalletID.Valid {
					return t.ID, nil
				}
			}
			return 0, nil
		})
		if err != nil {
			return sharedmodel.WrapErr("resolve gateway tx", err)
		}
		txID = resolved
	}

	// Step 2: update DB tx + auto-promote session (skip if no gateway tx).
	if txID != 0 {
		switch params.Outcome {
		case "paid":
			if err := b.markTxSuccess(ctx, markTxSuccessParams{TxID: txID}); err != nil {
				return sharedmodel.WrapErr("mark tx success", err)
			}
		case "failed":
			if err := b.markTxFailed(ctx, markTxFailedParams{TxID: txID, Reason: "gateway"}); err != nil {
				return sharedmodel.WrapErr("mark tx failed", err)
			}
		}
	}

	// Step 3: signal owning workflow's payment_event promise.
	wfName, wfID := WorkflowForSession(session)
	if wfName == "" {
		// Payout / internal sessions — no workflow to signal.
		return nil
	}

	restate.WorkflowSend(ctx, wfName, wfID, "ResolvePayment").Send(PaymentEvent{
		Kind: params.Outcome,
		TxID: txID,
	})
	return nil
}
