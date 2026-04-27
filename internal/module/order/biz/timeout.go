package orderbiz

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	restate "github.com/restatedev/sdk-go"
	"github.com/samber/lo"

	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// TimeoutCheckoutSession is fired by a Restate delayed send (paymentExpiry) after a
// Pending checkout session is created. If the session is still Pending, it marks
// it Failed, cancels all items in the session, releases inventory, and credits back
// any wallet portion that was already deducted.
func (b *OrderHandler) TimeoutCheckoutSession(ctx restate.Context, params TimeoutCheckoutSessionParams) error {
	session, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(ctx, null.IntFrom(params.SessionID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get session", err)
	}
	// Idempotent: already terminal (paid / failed / cancelled).
	if session.Status != orderdb.OrderStatusPending {
		return nil
	}

	items, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderItem, error) {
		return b.storage.Querier().ListItemsByPaymentSession(ctx, params.SessionID)
	})
	if err != nil {
		return sharedmodel.WrapErr("list items by payment session", err)
	}

	// Mark session Failed and cancel items in a single durable step.
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		if _, err := b.storage.Querier().MarkPaymentSessionFailed(ctx, params.SessionID); err != nil {
			return sharedmodel.WrapErr("mark session failed", err)
		}
		for _, it := range items {
			if _, err := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
				ID:            it.ID,
				CancelledByID: uuid.NullUUID{},
			}); err != nil {
				return sharedmodel.WrapErr("cancel item", err)
			}
		}
		return nil
	}); err != nil {
		return sharedmodel.WrapErr("fail session + cancel items", err)
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

	// Credit buyer's settled portion back. CreditFromSession sums positive Success
	// txs only — pending/failed gateway leg contributes nothing, no minting risk.
	if session.FromID.Valid {
		if _, err := b.CreditFromSession(ctx, CreditFromSessionParams{
			SessionID:  params.SessionID,
			AccountID:  session.FromID.UUID,
			CreditType: "Refund",
			Note:       "checkout timeout wallet refund",
		}); err != nil {
			return sharedmodel.WrapErr("wallet credit timeout refund", err)
		}
	}

	// Notify buyer.
	if session.FromID.Valid && len(items) > 0 {
		itemNames := lo.Map(items, func(it orderdb.OrderItem, _ int) string { return it.SkuName })
		summary := ordermodel.SummarizeNames(itemNames)
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: session.FromID.UUID,
			Type:      accountmodel.NotiOrderCancelled,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Payment expired",
			Content:   fmt.Sprintf("Your checkout for %s was cancelled because payment was not received in time.", summary),
		})
	}

	return nil
}

// TimeoutConfirmFeeSession is fired by a Restate delayed send (paymentExpiry) after a
// Pending confirm-fee session is created. If the session is still Pending:
// - Marks confirm-fee session Failed
// - Marks the associated payout session Failed
// - Unlinks items from the order (order_id → NULL)
// - Deletes the order row
// - Credits back any wallet portion the seller already paid
func (b *OrderHandler) TimeoutConfirmFeeSession(ctx restate.Context, params TimeoutConfirmFeeSessionParams) error {
	session, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(ctx, null.IntFrom(params.SessionID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get session", err)
	}
	if session.Status != orderdb.OrderStatusPending {
		return nil
	}

	order, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderOrder, error) {
		return b.storage.Querier().GetOrder(ctx, toNullUUID(&params.OrderID))
	})
	if err != nil {
		return sharedmodel.WrapErr("get order", err)
	}

	// Rollback: mark sessions Failed, unlink items, delete order — all in one durable step.
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		if _, err := b.storage.Querier().MarkPaymentSessionFailed(ctx, params.SessionID); err != nil {
			return sharedmodel.WrapErr("mark confirm-fee session failed", err)
		}
		if payoutSession, perr := b.storage.Querier().GetPendingPayoutSessionForOrder(ctx, order.ID); perr == nil {
			if _, err := b.storage.Querier().MarkPaymentSessionFailed(ctx, payoutSession.ID); err != nil {
				return sharedmodel.WrapErr("mark payout session failed", err)
			}
		}
		if err := b.storage.Querier().UnlinkItemsFromOrder(ctx, toNullUUID(&order.ID)); err != nil {
			return sharedmodel.WrapErr("unlink items from order", err)
		}
		if err := b.storage.Querier().DeleteOrder(ctx, orderdb.DeleteOrderParams{ID: []uuid.UUID{order.ID}}); err != nil {
			return sharedmodel.WrapErr("delete order", err)
		}
		return nil
	}); err != nil {
		return sharedmodel.WrapErr("rollback confirm fee", err)
	}

	// Credit seller's settled portion back via the same shared helper.
	if session.FromID.Valid {
		if _, err := b.CreditFromSession(ctx, CreditFromSessionParams{
			SessionID:  params.SessionID,
			AccountID:  session.FromID.UUID,
			CreditType: "Refund",
			Note:       "confirm fee timeout wallet refund",
		}); err != nil {
			return sharedmodel.WrapErr("wallet credit confirm-fee timeout refund", err)
		}
	}

	return nil
}
