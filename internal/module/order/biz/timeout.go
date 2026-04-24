package orderbiz

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// TimeoutCheckoutTx is fired by a Restate delayed send (paymentExpiry) after a Pending
// checkout gateway tx is created. If the tx is still Pending, it marks it Failed,
// cancels all items that reference it, releases inventory, and credits back any wallet
// portion that was already deducted.
func (b *OrderHandler) TimeoutCheckoutTx(ctx restate.Context, params TimeoutCheckoutTxParams) error {
	tx, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderTransaction, error) {
		return b.storage.Querier().GetTransaction(ctx, null.IntFrom(params.TxID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get tx", err)
	}
	// Idempotent: already processed (paid or otherwise terminal).
	if tx.Status != orderdb.OrderStatusPending {
		return nil
	}

	items, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderItem, error) {
		return b.storage.Querier().ListItemsByPaymentTx(ctx, params.TxID)
	})
	if err != nil {
		return sharedmodel.WrapErr("list items by payment tx", err)
	}

	// Mark tx Failed and cancel items in a single durable step.
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		if _, err := b.storage.Querier().MarkTransactionFailed(ctx, params.TxID); err != nil {
			return sharedmodel.WrapErr("mark tx failed", err)
		}
		for _, it := range items {
			if _, err := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
				ID:            it.ID,
				CancelledByID: uuid.NullUUID{},
				RefundTxID:    null.Int{},
			}); err != nil {
				return sharedmodel.WrapErr("cancel item", err)
			}
		}
		return nil
	}); err != nil {
		return sharedmodel.WrapErr("fail tx + cancel items", err)
	}

	// Release inventory outside Run (cross-module Restate call).
	releaseItems := lo.Map(items, func(it orderdb.OrderItem, _ int) inventorybiz.ReleaseInventoryItem {
		return inventorybiz.ReleaseInventoryItem{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   it.SkuID,
			Amount:  it.Quantity,
		}
	})
	if err := b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{Items: releaseItems}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Credit buyer's wallet portion back if this was a hybrid checkout (wallet + gateway).
	// The wallet sibling tx was already Success; we refund it now that the gateway blocker failed.
	if tx.FromID.Valid {
		siblings, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderTransaction, error) {
			return b.storage.Querier().ListCheckoutSiblingsForTx(ctx, params.TxID)
		})
		if err == nil {
			var totalWallet int64
			for _, s := range siblings {
				// Wallet tx: status=Success and no payment_option (no gateway instrument)
				if s.Status == orderdb.OrderStatusSuccess && !s.PaymentOption.Valid {
					totalWallet += s.Amount
				}
			}
			if totalWallet > 0 {
				if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
					AccountID: tx.FromID.UUID,
					Amount:    totalWallet,
					Type:      "Refund",
					Reference: fmt.Sprintf("tx-timeout:%d", params.TxID),
					Note:      "checkout timeout wallet refund",
				}); err != nil {
					return sharedmodel.WrapErr("wallet credit timeout refund", err)
				}
			}
		}
	}

	// Notify buyer.
	if tx.FromID.Valid && len(items) > 0 {
		itemNames := lo.Map(items, func(it orderdb.OrderItem, _ int) string { return it.SkuName })
		summary := ordermodel.SummarizeNames(itemNames)
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: tx.FromID.UUID,
			Type:      accountmodel.NotiOrderCancelled,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Payment expired",
			Content:   fmt.Sprintf("Your checkout for %s was cancelled because payment was not received in time.", summary),
		})
	}

	return nil
}

// TimeoutConfirmFeeTx is fired by a Restate delayed send (paymentExpiry) after a Pending
// confirm_fee gateway tx is created. If the tx is still Pending:
// - Marks confirm_fee tx Failed
// - Marks the associated payout tx Failed
// - Unlinks items from the order (order_id → NULL)
// - Deletes the order row
// - Credits back any wallet portion the seller already paid
func (b *OrderHandler) TimeoutConfirmFeeTx(ctx restate.Context, params TimeoutConfirmFeeTxParams) error {
	tx, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderTransaction, error) {
		return b.storage.Querier().GetTransaction(ctx, null.IntFrom(params.TxID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get tx", err)
	}
	if tx.Status != orderdb.OrderStatusPending {
		return nil
	}

	order, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderOrder, error) {
		return b.storage.Querier().GetOrder(ctx, toNullUUID(&params.OrderID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get order", err)
	}

	// Rollback: mark txs Failed, unlink items, delete order — all in one durable step.
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		if _, err := b.storage.Querier().MarkTransactionFailed(ctx, params.TxID); err != nil {
			return sharedmodel.WrapErr("mark confirm_fee tx failed", err)
		}
		if payout, err := b.storage.Querier().GetPendingPayoutTxForOrder(ctx, toNullUUID(&order.ID)); err == nil {
			if _, err := b.storage.Querier().MarkTransactionFailed(ctx, payout.ID); err != nil {
				return sharedmodel.WrapErr("mark payout tx failed", err)
			}
		}
		if err := b.storage.Querier().UnlinkItemsFromOrder(ctx, toNullUUID(&order.ID)); err != nil {
			return sharedmodel.WrapErr("unlink items from order", err)
		}
		if err := b.storage.Querier().DeleteOrder(ctx, orderdb.DeleteOrderParams{ID: order.ID}); err != nil {
			return sharedmodel.WrapErr("delete order", err)
		}
		return nil
	}); err != nil {
		return sharedmodel.WrapErr("rollback confirm fee", err)
	}

	// Credit seller's wallet portion back if this was a hybrid confirm_fee.
	if tx.FromID.Valid {
		siblings, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderTransaction, error) {
			return b.storage.Querier().ListConfirmFeeSiblingsForTx(ctx, params.TxID)
		})
		if err == nil {
			var totalWallet int64
			for _, s := range siblings {
				if s.Status == orderdb.OrderStatusSuccess && !s.PaymentOption.Valid {
					totalWallet += s.Amount
				}
			}
			if totalWallet > 0 {
				if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
					AccountID: tx.FromID.UUID,
					Amount:    totalWallet,
					Type:      "Refund",
					Reference: fmt.Sprintf("confirm-fee-timeout:%d", params.TxID),
					Note:      "confirm fee timeout wallet refund",
				}); err != nil {
					return sharedmodel.WrapErr("wallet credit confirm-fee timeout refund", err)
				}
			}
		}
	}

	return nil
}
