package orderbiz

import (
	"encoding/json"
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
		sessionIDs := lo.Uniq(lo.Map(enriched, func(it ordermodel.OrderItem, _ int) int64 { return it.PaymentSessionID }))
		sessions, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderPaymentSession, error) {
			return b.storage.Querier().ListPaymentSession(ctx, orderdb.ListPaymentSessionParams{ID: sessionIDs})
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("db fetch payment sessions", err)
		}
		sessionMap := lo.KeyBy(sessions, func(s orderdb.OrderPaymentSession) int64 { return s.ID })
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

// CancelBuyerPending cancels a pending item, releases inventory, creates a refund tx, and credits wallet.
func (b *OrderHandler) CancelBuyerPending(ctx restate.Context, params CancelBuyerPendingParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate cancel pending item", err)
	}

	// Fetch and validate item.
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

	// Release inventory (outside Run — cross-module).
	if err = b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
		Items: []inventorybiz.ReleaseInventoryItem{{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   item.SkuID,
			Amount:  item.Quantity,
		}},
	}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Resolve buyer currency (outside Run — cross-module).
	buyerCurrency, err := b.inferCurrency(ctx, params.AccountID)
	if err != nil {
		return sharedmodel.WrapErr("infer buyer currency", err)
	}

	// Read source payment session — only sessions whose status is Success are
	// eligible for a refund. A still-Pending session means the buyer never paid
	// (gateway didn't complete); a Failed session means payment was rejected.
	// Either way, no money moved into the platform — we must not create a refund.
	paymentSession, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(ctx, null.IntFrom(item.PaymentSessionID))
	})
	if err != nil {
		return sharedmodel.WrapErr("db get payment session", err)
	}
	sessionSettled := paymentSession.Status == orderdb.OrderStatusSuccess

	// Find the original (positive-amount, Success, no reverses_id) tx in this session
	// to use as the reverses_id target. Assumption: single original tx per session
	// (refund split-tender not supported per design).
	var originalTxID null.Int
	if sessionSettled {
		sessionTxs, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderTransaction, error) {
			return b.storage.Querier().ListTransactionsBySession(ctx, paymentSession.ID)
		})
		if err != nil {
			return sharedmodel.WrapErr("db list session txs", err)
		}
		for _, tx := range sessionTxs {
			if tx.Status == orderdb.OrderStatusSuccess && tx.Amount > 0 && !tx.ReversesID.Valid {
				originalTxID = null.IntFrom(tx.ID)
				break
			}
		}
		if !originalTxID.Valid {
			return sharedmodel.WrapErr("no settled original tx in session", ordermodel.ErrOrderItemNotFound)
		}
	}

	// Create refund tx (only when buyer actually paid) and cancel item atomically.
	type cancelResult struct {
		RefundTx orderdb.OrderTransaction `json:"refund_tx"`
	}
	cancelRes, err := restate.Run(ctx, func(ctx restate.RunContext) (cancelResult, error) {
		var refundTx orderdb.OrderTransaction
		if sessionSettled && item.TotalAmount > 0 {
			var txErr error
			refundTx, txErr = b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     paymentSession.ID,
				Status:        orderdb.OrderStatusSuccess,
				Note:          "buyer cancel pre-confirm",
				Error:         null.String{},
				PaymentOption: null.String{},
				WalletID:      uuid.NullUUID{},
				Data:          json.RawMessage("{}"),
				Amount:        -item.TotalAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  mustNumericOne(),
				ReversesID:    originalTxID,
				DateSettled:   null.TimeFrom(time.Now()),
				DateExpired:   null.Time{},
			})
			if txErr != nil {
				return cancelResult{}, sharedmodel.WrapErr("db create refund tx", txErr)
			}
		}

		// If the session is still Pending, mark it Cancelled so the timeout
		// timer / webhook no-ops when it eventually fires.
		if paymentSession.Status == orderdb.OrderStatusPending {
			if _, err := b.storage.Querier().MarkPaymentSessionCancelled(ctx, paymentSession.ID); err != nil {
				return cancelResult{}, sharedmodel.WrapErr("db cancel pending session", err)
			}
		}

		if _, err := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
			CancelledByID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
			ID:            params.ItemID,
		}); err != nil {
			return cancelResult{}, sharedmodel.WrapErr("db cancel item", err)
		}

		return cancelResult{RefundTx: refundTx}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("cancel item", err)
	}

	// Credit buyer wallet (outside Run — cross-module). Routed through the
	// guard helper so we never mint balance for a buyer whose payment never
	// actually settled.
	if cancelRes.RefundTx.ID != 0 {
		if _, err := b.CreditFromSession(ctx, CreditFromSessionParams{
			SessionID:  item.PaymentSessionID,
			AccountID:  params.AccountID,
			CreditType: "Refund",
			Note:       "buyer cancel pre-confirm refund",
		}); err != nil {
			return sharedmodel.WrapErr("credit buyer", err)
		}
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
