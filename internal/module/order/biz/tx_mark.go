package orderbiz

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
)

// MarkTxSuccess transitions a Pending transaction to Success, recording the payment timestamp.
// Called by payment gateway webhooks.
func (b *OrderHandler) MarkTxSuccess(ctx restate.Context, params MarkTxSuccessParams) error {
	dateAt := params.DateAt
	if dateAt.IsZero() {
		dateAt = time.Now()
	}
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().MarkTransactionSuccess(ctx, orderdb.MarkTransactionSuccessParams{
			ID:       params.TxID,
			DatePaid: dateAt,
		})
		return err
	})
}

// MarkTxFailed transitions a Pending transaction to Failed.
// Called by payment gateway webhooks on decline / error.
func (b *OrderHandler) MarkTxFailed(ctx restate.Context, params MarkTxFailedParams) error {
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().MarkTransactionFailed(ctx, params.TxID)
		return err
	})
}
