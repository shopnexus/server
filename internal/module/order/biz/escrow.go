package orderbiz

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// ReleaseEscrow is fired by a Restate 7-day delayed send after transport.status=Delivered.
// It marks the payout session and its child tx Success and credits the seller's wallet,
// UNLESS an active refund exists on any item — in which case it silently skips (the refund
// flow will later cancel the payout on approve, or re-schedule release on reject).
func (b *OrderHandler) ReleaseEscrow(ctx restate.Context, params ReleaseEscrowParams) error {
	order, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderOrder, error) {
		return b.storage.Querier().GetOrder(ctx, toNullUUID(&params.OrderID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get order", err)
	}

	hasActive, err := restate.Run(ctx, func(ctx restate.RunContext) (bool, error) {
		return b.storage.Querier().HasActiveRefundForOrder(ctx, params.OrderID)
	})
	if err != nil {
		return sharedmodel.WrapErr("check active refund", err)
	}
	if hasActive {
		// Refund flow will handle payout status; no-op here.
		return nil
	}

	payoutSession, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		s, err := b.storage.Querier().GetPendingPayoutSessionForOrder(ctx, params.OrderID)
		if err != nil {
			return orderdb.OrderPaymentSession{}, ordermodel.ErrTxNotFound.Terminal()
		}
		return s, nil
	})
	if err != nil {
		return err
	}

	sessionTxs, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderTransaction, error) {
		return b.storage.Querier().ListTransactionsBySession(ctx, payoutSession.ID)
	})
	if err != nil {
		return sharedmodel.WrapErr("list payout session txs", err)
	}
	var pendingTxID int64
	for _, tx := range sessionTxs {
		if tx.Status == orderdb.OrderStatusPending && tx.Amount > 0 && !tx.ReversesID.Valid {
			pendingTxID = tx.ID
			break
		}
	}
	if pendingTxID == 0 {
		return ordermodel.ErrTxNotFound.Terminal()
	}

	// MarkTxSuccess auto-promotes the payout session to Success once this last
	// Pending tx settles — no separate MarkPaymentSessionSuccess needed.
	if err := b.MarkTxSuccess(ctx, MarkTxSuccessParams{TxID: pendingTxID, DateAt: time.Now()}); err != nil {
		return sharedmodel.WrapErr("mark payout tx success", err)
	}

	if _, err := b.CreditFromSession(ctx, CreditFromSessionParams{
		SessionID:  payoutSession.ID,
		AccountID:  order.SellerID,
		CreditType: "Payout",
		Note:       "escrow release",
	}); err != nil {
		return sharedmodel.WrapErr("wallet credit seller", err)
	}
	return nil
}
