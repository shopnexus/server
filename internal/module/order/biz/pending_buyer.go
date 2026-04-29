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
	"shopnexus-server/internal/shared/saga"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// hydrateItems fetches items by IDs and enriches them with product resources.
func (b *OrderHandler) hydrateItems(ctx restate.Context, itemIDs []int64) ([]ordermodel.OrderItem, error) {
	if len(itemIDs) == 0 {
		return []ordermodel.OrderItem{}, nil
	}

	dbItems, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderItem, error) {
		return b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			ID: itemIDs,
		})
	})
	if err != nil {
		return nil, err
	}

	return b.enrichItems(dbItems)
}

// enrichItems converts DB items to model items (no separate resources enrichment needed here).
func (b *OrderHandler) enrichItems(dbItems []orderdb.OrderItem) ([]ordermodel.OrderItem, error) {
	if len(dbItems) == 0 {
		return []ordermodel.OrderItem{}, nil
	}

	result := make([]ordermodel.OrderItem, 0, len(dbItems))
	for _, it := range dbItems {
		result = append(result, mapOrderItem(it))
	}

	return result, nil
}

// ListBuyerPendingItems returns paginated paid pending items for the buyer.
func (b *OrderHandler) ListBuyerPendingItems(
	ctx restate.Context,
	params ListBuyerPendingItemsParams,
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list pending items", err)
	}

	type pendingResult struct {
		Items []orderdb.OrderItem `json:"items"`
		Total int64               `json:"total"`
	}

	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (pendingResult, error) {
		items, err := b.storage.Querier().ListBuyerPendingItems(ctx, params.AccountID)
		if err != nil {
			return pendingResult{}, err
		}

		total, err := b.storage.Querier().CountBuyerPendingItems(ctx, params.AccountID)
		if err != nil {
			return pendingResult{}, err
		}

		return pendingResult{Items: items, Total: total}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list pending items", err)
	}

	enriched, err := b.enrichItems(dbResult.Items)
	if err != nil {
		return zero, sharedmodel.WrapErr("enrich pending items", err)
	}

	// Attach the payment session so the FE can branch on its status —
	// "Awaiting Payment" + Continue Payment when Pending, "Awaiting Seller" when Success.
	if len(enriched) > 0 {
		sessionIDs := lo.Uniq(lo.Map(enriched, func(it ordermodel.OrderItem, _ int) uuid.UUID { return it.PaymentSessionID }))
		sessions, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderPaymentSession, error) {
			return b.storage.Querier().ListPaymentSession(ctx, orderdb.ListPaymentSessionParams{ID: sessionIDs})
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("db fetch payment sessions", err)
		}
		sessionMap := lo.KeyBy(sessions, func(s orderdb.OrderPaymentSession) uuid.UUID { return s.ID })
		for i := range enriched {
			if s, ok := sessionMap[enriched[i].PaymentSessionID]; ok {
				mapped := mapPaymentSession(s)
				enriched[i].PaymentSession = &mapped
			}
		}
	}

	var totalVal null.Int64
	totalVal.SetValid(dbResult.Total)

	return sharedmodel.PaginateResult[ordermodel.OrderItem]{
		PageParams: params.PaginationParams,
		Total:      totalVal,
		Data:       enriched,
	}, nil
}

// CancelBuyerPending cancels a pre-confirm pending item. Branches on the
// item's payment session status:
//
//   - Pending → CheckoutWorkflow is still alive in WaitFirst; signal its
//     user_cancel promise and let the workflow saga compensate (release
//     inventory, mark session Failed, cancel all items, credit wallet from
//     any settled legs). Cancels the entire checkout session.
//
//   - Success → workflow has exited successfully (buyer paid, awaiting
//     seller confirm). No saga to run. Issue a partial refund for this
//     item only: create a reversing refund tx in the session, release
//     inventory, mark item cancelled, credit buyer wallet via
//     CreditFromSession. Sibling items in the session stay active.
func (b *OrderHandler) CancelBuyerPending(ctx restate.Context, params CancelBuyerPendingParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate cancel pending item", err)
	}

	item, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderItem, error) {
		var zero orderdb.OrderItem
		dbItem, err := b.storage.Querier().GetItem(ctx, null.IntFrom(params.ItemID))
		if err != nil {
			return zero, sharedmodel.WrapErr("db get item", err)
		}
		if dbItem.AccountID != params.AccountID {
			return zero, ordermodel.ErrOrderItemNotFound.Terminal()
		}
		if dbItem.OrderID.Valid {
			return zero, ordermodel.ErrItemAlreadyConfirmed
		}
		if dbItem.DateCancelled.Valid {
			return zero, ordermodel.ErrItemAlreadyCancelled
		}
		return dbItem, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch item", err)
	}

	paymentSession, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(ctx, uuid.NullUUID{UUID: item.PaymentSessionID, Valid: true})
	})
	if err != nil {
		return sharedmodel.WrapErr("db get payment session", err)
	}

	switch paymentSession.Status {
	case orderdb.OrderStatusPending:
		// Workflow is still in WaitFirst — signal cancel and let saga compensate.
		restate.WorkflowSend(ctx, "CheckoutWorkflow", item.PaymentSessionID.String(), "CancelCheckout").
			Send(struct{}{})

	case orderdb.OrderStatusSuccess:
		// Workflow exited; partial refund this single item.
		if err = b.RefundPendingItem(ctx, RefundPendingItemParams{
			Item:             item,
			PaymentSessionID: paymentSession.ID,
		}); err != nil {
			return err
		}

	default:
		return ordermodel.ErrItemAlreadyCancelled.Terminal()
	}

	// Notify seller (fire-and-forget).
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: item.SellerID,
		Type:      accountmodel.NotiPendingItemCancelled,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Pending item cancelled",
		Content:   "A buyer has cancelled a pending item.",
	})

	return nil
}

type RefundPendingItemParams struct {
	Item             orderdb.OrderItem
	PaymentSessionID uuid.UUID
}

// RefundPendingItem refunds a single paid-but-not-yet-confirmed item
// without touching its sibling items in the same payment session.
//
// Saga ordering: each compensable side effect runs AFTER its compensator is
// deferred, and the atomic Run (refund tx + cancel item) is placed last so it
// needs no compensator. If any step fails, the deferred top-level guard fires
// saga.Compensate() which unwinds LIFO via restate.RunVoid (idempotent retry).
//
//	1. find original positive Success tx (read-only, no compensator)
//	2. release inventory  → defer re-reserve
//	3. wallet credit      → defer wallet debit
//	4. refund tx + cancel item (last action, no compensator)
func (b *OrderHandler) RefundPendingItem(
	ctx restate.Context,
	params RefundPendingItemParams,
) error {
	var err error

	sagaTx := saga.New(ctx)
	defer func() {
		if err != nil {
			sagaTx.Compensate()
		}
	}()

	var buyerCurrency string
	buyerCurrency, err = b.InferCurrency(ctx, params.Item.AccountID)
	if err != nil {
		return sharedmodel.WrapErr("infer buyer currency", err)
	}

	// Step 1: find the original positive Success tx — refund leg reverses it.
	// Single original tx per session (no split-tender).
	var originalTxID null.Int
	originalTxID, err = restate.Run(ctx, func(rctx restate.RunContext) (null.Int, error) {
		var txs []orderdb.OrderTransaction
		txs, err = b.storage.Querier().ListTransactionsBySession(rctx, params.PaymentSessionID)
		if err != nil {
			return null.Int{}, err
		}
		for _, tx := range txs {
			if tx.Status == orderdb.OrderStatusSuccess && tx.Amount > 0 && !tx.ReversesID.Valid {
				return null.IntFrom(tx.ID), nil
			}
		}
		return null.Int{}, ordermodel.ErrOrderItemNotFound
	})
	if err != nil {
		return sharedmodel.WrapErr("find original tx", err)
	}

	// Step 2: release inventory. Compensator re-reserves so cancellation rollback
	// restores stock state.
	sagaTx.Defer("reserve_inventory", func(rctx restate.RunContext) error {
		_, e := b.inventory.ReserveInventory(rctx, inventorybiz.ReserveInventoryParams{
			Items: []inventorybiz.ReserveInventoryItem{{
				RefType: inventorydb.InventoryStockRefTypeProductSku,
				RefID:   params.Item.SkuID,
				Amount:  params.Item.Quantity,
			}},
		})
		return e
	})
	if err = b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
		Items: []inventorybiz.ReleaseInventoryItem{{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   params.Item.SkuID,
			Amount:  params.Item.Quantity,
		}},
	}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Step 3: credit only the partial item amount. CreditFromSession would
	// over-credit (it sums all positive Success txs in the session, ignoring
	// our negative refund leg). Direct WalletCredit with an item-scoped
	// Reference is the right primitive for partial refunds.
	creditRef := fmt.Sprintf("partial-refund:item:%d", params.Item.ID)
	sagaTx.Defer("wallet_debit", func(rctx restate.RunContext) error {
		_, e := b.account.WalletDebit(rctx, accountbiz.WalletDebitParams{
			AccountID: params.Item.AccountID,
			Amount:    params.Item.TotalAmount,
			Reference: "rollback:" + creditRef,
			Note:      "rollback partial refund credit",
		})
		return e
	})
	if err = b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
		AccountID: params.Item.AccountID,
		Amount:    params.Item.TotalAmount,
		Type:      "Refund",
		Reference: creditRef,
		Note:      "buyer cancel pre-confirm partial refund",
	}); err != nil {
		return sharedmodel.WrapErr("credit buyer wallet", err)
	}

	// Step 4 (last, no compensator): atomic refund tx + cancel item.
	if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
		if _, e := b.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
			SessionID:    params.PaymentSessionID,
			Status:       orderdb.OrderStatusSuccess,
			Note:         "buyer cancel pre-confirm",
			Data:         json.RawMessage("{}"),
			Amount:       -params.Item.TotalAmount,
			FromCurrency: buyerCurrency,
			ToCurrency:   buyerCurrency,
			ExchangeRate: mustNumericOne(),
			ReversesID:   originalTxID,
			DateSettled:  null.TimeFrom(time.Now()),
		}); e != nil {
			return sharedmodel.WrapErr("db create refund tx", e)
		}
		if _, e := b.storage.Querier().CancelItem(rctx, orderdb.CancelItemParams{
			CancelledByID: uuid.NullUUID{UUID: params.Item.AccountID, Valid: true},
			ID:            params.Item.ID,
		}); e != nil {
			return sharedmodel.WrapErr("db cancel item", e)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}
