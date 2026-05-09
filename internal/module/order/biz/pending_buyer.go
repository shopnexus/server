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
	"shopnexus-server/internal/shared/idempotency"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/saga"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

// ListBuyerPendingItems returns paginated paid pending items for the buyer.
func (b *OrderHandler) ListBuyerPendingItems(
	ctx restate.Context,
	params ListBuyerPendingItemsParams,
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.PaginateResult[ordermodel.OrderItem]{}, sharedmodel.WrapErr("validate list pending items", err)
	}
	return b.listBuyerItems(ctx, params.PaginationParams, params.AccountID,
		func(rctx restate.RunContext, accountID uuid.UUID) ([]orderdb.OrderItem, int64, error) {
			items, err := b.storage.Querier().ListBuyerPendingItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			total, err := b.storage.Querier().CountBuyerPendingItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			return items, total, nil
		})
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

// RefundPendingItem refunds a single paid item (not yet confirmed).
func (b *OrderHandler) RefundPendingItem(
	ctx restate.Context,
	params RefundPendingItemParams,
) error {
	sagaTx := saga.New(ctx)

	return sagaTx.Wrap(func() error {
		var err error
		var buyerCurrency string
		buyerCurrency, err = b.InferCurrency(ctx, params.Item.AccountID)
		if err != nil {
			return sharedmodel.WrapErr("infer buyer currency", err)
		}

		// Step 1: find the original positive Success tx — refund leg reverses it.
		// Single original tx per session (no split-tender).
		var originalTxID uuid.NullUUID
		originalTxID, err = restate.Run(ctx, func(rctx restate.RunContext) (uuid.NullUUID, error) {
			var txs []orderdb.OrderTransaction
			txs, err = b.storage.Querier().ListTransactionsBySession(rctx, params.PaymentSessionID)
			if err != nil {
				return uuid.NullUUID{}, err
			}
			for _, tx := range txs {
				if tx.Status == orderdb.OrderStatusSuccess && tx.Amount > 0 && !tx.ReversesID.Valid {
					return uuid.NullUUID{UUID: tx.ID, Valid: true}, nil
				}
			}
			return uuid.NullUUID{}, ordermodel.ErrOrderItemNotFound
		})
		if err != nil {
			return sharedmodel.WrapErr("find original tx", err)
		}

		// Step 2: release inventory
		// Saga key paired across forward (Release, claims) and compensator (Reserve, consumes).
		releaseKey := restate.UUID(ctx)
		sagaTx.Defer("reserve_inventory", func(ctx restate.Context) error {
			return restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				_, e := b.inventory.ReserveInventory(rctx, inventorybiz.ReserveInventoryParams{
					Keys: idempotency.Keys{ConsumeKey: releaseKey},
					Items: []inventorybiz.ReserveInventoryItem{{
						RefType: inventorydb.InventoryStockRefTypeProductSku,
						RefID:   params.Item.SkuID,
						Amount:  params.Item.Quantity,
					}},
				})
				return e
			})
		})
		if err = b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
			Keys: idempotency.Keys{ClaimKey: releaseKey},
			Items: []inventorybiz.ReleaseInventoryItem{{
				RefType: inventorydb.InventoryStockRefTypeProductSku,
				RefID:   params.Item.SkuID,
				Amount:  params.Item.Quantity,
			}},
		}); err != nil {
			return sharedmodel.WrapErr("release inventory", err)
		}

		// Step 3: credit only the partial item amount
		// Compensator debits the same amount
		creditRef := fmt.Sprintf("partial-refund:item:%d", params.Item.ID)
		sagaTx.Defer("wallet_debit", func(ctx restate.Context) error {
			return restate.RunVoid(ctx, func(rctx restate.RunContext) error {
				_, e := b.account.WalletDebit(rctx, accountbiz.WalletDebitParams{
					AccountID: params.Item.AccountID,
					Amount:    params.Item.TotalAmount,
					Reference: "rollback:" + creditRef,
					Note:      "rollback partial refund credit",
				})
				return e
			})
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
			txStorage, e := b.storage.BeginTx(rctx)
			if e != nil {
				return sharedmodel.WrapErr("begin tx", e)
			}

			if _, e = txStorage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
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
			if _, e = txStorage.Querier().CancelItem(rctx, orderdb.CancelItemParams{
				CancelledByID: uuid.NullUUID{UUID: params.Item.AccountID, Valid: true},
				ID:            params.Item.ID,
			}); e != nil {
				return sharedmodel.WrapErr("db cancel item", e)
			}

			return txStorage.Commit(rctx)
		}); err != nil {
			return sharedmodel.WrapErr("refund tx + cancel item", err)
		}

		return nil
	})
}
