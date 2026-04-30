package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// RejectSellerPending rejects pending items owned by the seller, releases inventory, and refunds buyers.
func (b *OrderHandler) RejectSellerPending(ctx restate.Context, params RejectSellerPendingParams) error {
	// Lock: exclusive — same key as ConfirmSellerPending.
	unlock := b.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", params.Account.ID))
	defer unlock()

	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate reject items", err)
	}

	sellerID := params.Account.ID

	// Fetch and validate items.
	items, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderItem, error) {
		dbItems, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			ID: params.ItemIDs,
		})
		if err != nil {
			return nil, sharedmodel.WrapErr("db list items", err)
		}
		if len(dbItems) != len(params.ItemIDs) {
			return nil, ordermodel.ErrOrderItemNotFound.Terminal()
		}

		for _, item := range dbItems {
			if item.OrderID.Valid {
				return nil, ordermodel.ErrItemAlreadyConfirmed
			}
			if item.DateCancelled.Valid {
				return nil, ordermodel.ErrItemAlreadyCancelled
			}
			if item.SellerID != sellerID {
				return nil, ordermodel.ErrItemNotOwnedBySeller
			}
		}
		return dbItems, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch items", err)
	}

	// Release inventory for each item (outside Run — cross-module).
	releaseItems := lo.Map(items, func(item orderdb.OrderItem, _ int) inventorybiz.ReleaseInventoryItem {
		return inventorybiz.ReleaseInventoryItem{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   item.SkuID,
			Amount:  item.Quantity,
		}
	})
	if err := b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
		Items: releaseItems,
	}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Group items by buyer and process refunds per buyer.
	buyerItems := make(map[uuid.UUID][]orderdb.OrderItem)
	for _, item := range items {
		buyerItems[item.AccountID] = append(buyerItems[item.AccountID], item)
	}

	for buyerID, buyerItemList := range buyerItems {
		itemIDs := lo.Map(buyerItemList, func(it orderdb.OrderItem, _ int) int64 { return it.ID })

		// Look up the payment session for every distinct item. We refund only
		// items whose session actually settled to Success — Pending/Failed
		// items had no money flow through the platform.
		sessionIDs := lo.Uniq(lo.Map(buyerItemList, func(it orderdb.OrderItem, _ int) uuid.UUID { return it.PaymentSessionID }))
		sessions, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderPaymentSession, error) {
			return b.storage.Querier().ListPaymentSession(ctx, orderdb.ListPaymentSessionParams{ID: sessionIDs})
		})
		if err != nil {
			return sharedmodel.WrapErr("db fetch payment sessions", err)
		}
		sessionByID := lo.KeyBy(sessions, func(s orderdb.OrderPaymentSession) uuid.UUID { return s.ID })

		// For each Success session, fetch its original tx (positive Success, no reverses_id)
		// to use as reverses_id on the refund leg. Per design: single original per session.
		originalTxBySession := make(map[uuid.UUID]uuid.UUID)
		for sid, s := range sessionByID {
			if s.Status != orderdb.OrderStatusSuccess {
				continue
			}
			sessionTxs, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderTransaction, error) {
				return b.storage.Querier().ListTransactionsBySession(ctx, sid)
			})
			if err != nil {
				return sharedmodel.WrapErr("db list session txs", err)
			}
			for _, tx := range sessionTxs {
				if tx.Status == orderdb.OrderStatusSuccess && tx.Amount > 0 && !tx.ReversesID.Valid {
					originalTxBySession[sid] = tx.ID
					break
				}
			}
		}

		type itemRefundPlan struct {
			Item       orderdb.OrderItem
			OriginalID uuid.UUID
		}
		var refundPlans []itemRefundPlan
		var pendingSessionIDs []uuid.UUID
		var totalRefund int64
		for _, item := range buyerItemList {
			s, ok := sessionByID[item.PaymentSessionID]
			if !ok {
				continue
			}
			switch s.Status {
			case orderdb.OrderStatusSuccess:
				if origID, hasOrig := originalTxBySession[s.ID]; hasOrig {
					refundPlans = append(refundPlans, itemRefundPlan{Item: item, OriginalID: origID})
					totalRefund += item.TotalAmount
				}
			case orderdb.OrderStatusPending:
				pendingSessionIDs = append(pendingSessionIDs, s.ID)
			}
		}
		pendingSessionIDs = lo.Uniq(pendingSessionIDs)

		// Infer buyer currency before the durable Run (outside Run — cross-module).
		buyerCurrency, err := b.InferCurrency(ctx, buyerID)
		if err != nil {
			return sharedmodel.WrapErr("infer buyer currency", err)
		}

		// Create per-session refund txs and cancel each item atomically.
		refundTxIDs, err := restate.Run(ctx, func(ctx restate.RunContext) ([]uuid.UUID, error) {
      var txIDs []uuid.UUID
			// One refund leg per item, in its own session, reversing the original tx.
			for _, plan := range refundPlans {
				if plan.Item.TotalAmount <= 0 {
					continue
				}
				tx, txErr := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
					SessionID:     plan.Item.PaymentSessionID,
					Status:        orderdb.OrderStatusSuccess,
					Note:          "seller reject pre-confirm",
					Error:         null.String{},
					PaymentOption: null.String{},
					Data:          json.RawMessage("{}"),
					Amount:        -plan.Item.TotalAmount,
					FromCurrency:  buyerCurrency,
					ToCurrency:    buyerCurrency,
					ExchangeRate:  mustNumericOne(),
					ReversesID:    uuid.NullUUID{UUID: plan.OriginalID, Valid: true},
					DateSettled:   null.TimeFrom(time.Now()),
					DateExpired:   null.Time{},
				})
				if txErr != nil {
					return nil, sharedmodel.WrapErr("db create refund tx", txErr)
				}
				txIDs = append(txIDs, tx.ID)
			}

			// Mark any Pending sessions as Cancelled so their timeout / webhook no-ops.
			for _, sid := range pendingSessionIDs {
				if _, err := b.storage.Querier().MarkPaymentSessionCancelled(ctx, sid); err != nil {
					return nil, sharedmodel.WrapErr("db cancel pending session", err)
				}
			}

			// Cancel each item with seller as cancelled_by_id.
			for _, id := range itemIDs {
				if _, err := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
					CancelledByID: uuid.NullUUID{UUID: sellerID, Valid: true},
					ID:            id,
				}); err != nil {
					return nil, sharedmodel.WrapErr("db cancel item", err)
				}
			}

			return txIDs, nil
		})
		if err != nil {
			return sharedmodel.WrapErr("reject items for buyer", err)
		}

		// Credit buyer wallet per session — CreditFromSession sums settled positive
		// txs in each session and skips no-ops, so unsettled sessions don't mint balance.
		_ = totalRefund // kept above only for the empty-list short-circuit clarity
		if len(refundTxIDs) > 0 {
			for _, plan := range refundPlans {
				if _, err := b.CreditFromSession(ctx, CreditFromSessionParams{
					SessionID:  plan.Item.PaymentSessionID,
					AccountID:  buyerID,
					CreditType: "Refund",
					Note:       "seller reject pre-confirm refund",
				}); err != nil {
					return sharedmodel.WrapErr("credit buyer from session", err)
				}
			}
		}

		// Notify buyer (fire-and-forget).
		rejectedNames := lo.Map(buyerItemList, func(it orderdb.OrderItem, _ int) string { return it.SkuName })
		rejectSummary := ordermodel.SummarizeNames(rejectedNames)
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: buyerID,
			Type:      accountmodel.NotiItemsRejected,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Items rejected",
			Content:   fmt.Sprintf("%s has been rejected by the seller.", rejectSummary),
			Metadata:  json.RawMessage(`{}`),
		})
	}

	return nil
}
