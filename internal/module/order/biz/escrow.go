package orderbiz

import (
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// ReleaseEscrow is fired by a Restate 7-day delayed send after transport.status=Delivered.
// It releases the payout tx to Success and credits the seller's wallet, UNLESS an active
// refund exists on any item of the order — in which case it silently skips (the refund flow
// will later set payout → Cancelled on approve, or re-schedule release on reject).
func (b *OrderHandler) ReleaseEscrow(ctx restate.Context, params ReleaseEscrowParams) error {
	order, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderOrder, error) {
		return b.storage.Querier().GetOrder(ctx, toNullUUID(&params.OrderID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get order", err)
	}

	hasActive, err := restate.Run(ctx, func(ctx restate.RunContext) (bool, error) {
		return b.storage.Querier().HasActiveRefundForOrder(ctx, toNullUUID(&params.OrderID))
	})
	if err != nil {
		return sharedmodel.WrapErr("check active refund", err)
	}
	if hasActive {
		// Refund flow will handle payout status; no-op here.
		return nil
	}

	payoutTx, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderTransaction, error) {
		tx, err := b.storage.Querier().GetPendingPayoutTxForOrder(ctx, toNullUUID(&params.OrderID))
		if err != nil {
			return orderdb.OrderTransaction{}, ordermodel.ErrTxNotFound.Terminal()
		}
		return b.storage.Querier().MarkTransactionSuccess(ctx, orderdb.MarkTransactionSuccessParams{
			ID:       tx.ID,
			DatePaid: time.Now(),
		})
	})
	if err != nil {
		return err
	}

	if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
		AccountID: order.SellerID,
		Amount:    payoutTx.Amount,
		Type:      "Payout",
		Reference: fmt.Sprintf("tx:%d", payoutTx.ID),
		Note:      "escrow release",
	}); err != nil {
		return sharedmodel.WrapErr("wallet credit seller", err)
	}
	return nil
}
