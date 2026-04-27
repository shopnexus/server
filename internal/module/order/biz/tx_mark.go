package orderbiz

import (
	"errors"
	"time"

	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// MarkTxSuccess transitions a Pending transaction to Success and, if this was the
// last Pending tx in its session (and no leg failed), auto-promotes the session
// to Success. Called by payment gateway webhooks; also used internally after
// successful cross-module debit/credit acknowledgements.
func (b *OrderHandler) MarkTxSuccess(ctx restate.Context, params MarkTxSuccessParams) error {
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

// MarkTxFailed transitions a Pending transaction to Failed.
// Called by payment gateway webhooks on decline / error.
func (b *OrderHandler) MarkTxFailed(ctx restate.Context, params MarkTxFailedParams) error {
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().MarkTransactionFailed(ctx, orderdb.MarkTransactionFailedParams{
			ID:    params.TxID,
			Error: null.NewString(params.Reason, params.Reason != ""),
		})
		return err
	})
}
