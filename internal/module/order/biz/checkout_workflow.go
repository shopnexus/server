package orderbiz

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commonmodel "shopnexus-server/internal/module/common/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/transport"
	sharedcurrency "shopnexus-server/internal/shared/currency"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/saga"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CheckoutWorkflowHandler is the Restate Workflow that drives a buyer checkout
// from "submitted" through "paid | expired | cancelled". It is structurally a
// port of OrderHandler.BuyerCheckout, with side effects guarded by saga
// compensators (see saga.go) and the payment outcome awaited via a durable
// promise + expiry timer.
type CheckoutWorkflowHandler struct {
	base *OrderHandler
}

// NewCheckoutWorkflowHandler wraps an OrderHandler so the workflow can reuse
// every helper (storage, locker, account/catalog/inventory/common clients,
// payment/transport maps, MarkTxSuccess, CreditFromSession, etc.). The base
// is held as a named field (not embedded) so reflection-driven Restate
// registration only sees workflow-typed methods on this struct.
func NewCheckoutWorkflowHandler(base *OrderHandler) *CheckoutWorkflowHandler {
	return &CheckoutWorkflowHandler{base: base}
}

// ServiceName registers this struct under "CheckoutWorkflow" for restate.Reflect.
func (h *CheckoutWorkflowHandler) ServiceName() string { return "CheckoutWorkflow" }

// Run is the workflow body. The HTTP transport allocates a UUID, uses its
// string form as the workflow ID when calling restate.Workflow(...), and
// the workflow recovers it via ctx.Key() — the session row is then INSERTed
// with that exact UUID. This keeps workflow ID, session ID, and webhook
// RefID in lockstep so callers can attach to the `payment_url` promise
// before Run reaches its publish step, and webhooks can route directly
// without a DB lookup.
//
// Saga semantics: every committed side effect (cart deletion, inventory
// reservation, session/tx/item rows, wallet debit) is paired with a Defer()
// compensator BEFORE the action runs. On the success path we Clear(); on
// terminal failure / cancel / expiry the LIFO compensators execute via
// restate.RunVoid (idempotent retry).
func (h *CheckoutWorkflowHandler) Run(
	ctx restate.WorkflowContext,
	input CheckoutWorkflowInput,
) (out CheckoutWorkflowOutput, err error) {
	defer metrics.TrackHandler("checkout_workflow", "Run", &err)()

	workflowID := restate.UUID(ctx)
	sessionID := workflowID

	// Step 1: Validate.
	if err = validator.Validate(input); err != nil {
		return out, sharedmodel.WrapErr("validate checkout", err)
	}
	if input.BuyNow && len(input.Items) != 1 {
		return out, ordermodel.ErrBuyNowSingleSkuOnly.Terminal()
	}

	saga := saga.New(ctx)
	var (
		cancelled                bool
		expired                  bool
		sessionIDForCompensation uuid.UUID
	)
	defer func() {
		if err != nil || cancelled || expired {
			saga.Compensate()
			// CreditFromSession is a cross-module call (WalletCredit) and must
			// run on the workflow context, not inside a saga RunVoid. We invoke
			// it here, after the saga has marked the session Failed and
			// cancelled items, so that the credit reflects only legs that
			// actually settled.
			if sessionIDForCompensation != uuid.Nil {
				if _, ce := h.base.CreditFromSession(ctx, CreditFromSessionParams{
					SessionID:  sessionIDForCompensation,
					AccountID:  input.Account.ID,
					CreditType: "Refund",
					Note:       "checkout saga compensation",
				}); ce != nil {
					slog.Error("checkout compensation: credit from session", slog.Any("error", ce))
				}
			}
			if err != nil && !restate.IsTerminalError(err) {
				err = restate.TerminalError(err)
			}
		}
	}()

	// Step 1.5: Load buyer profile and enforce address country match.
	buyerProfile, err := h.base.account.GetProfile(ctx, accountbiz.GetProfileParams{
		Issuer:    input.Account,
		AccountID: input.Account.ID,
	})
	if err != nil {
		return out, sharedmodel.WrapErr("load buyer profile", err)
	}

	resolvedCountry, err := h.base.common.ResolveCountry(ctx, input.Address)
	if err != nil {
		return out, err
	}
	if resolvedCountry != buyerProfile.Country {
		return out, ordermodel.ErrCheckoutAddressCountryMismatch.Fmt(resolvedCountry, buyerProfile.Country).Terminal()
	}

	skuIDs := lo.Map(input.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	checkoutItemMap := lo.KeyBy(input.Items, func(s CheckoutItem) uuid.UUID { return s.SkuID })

	// Step 2: Fetch product data (SKUs + SPUs).
	skus, err := h.base.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return out, sharedmodel.WrapErr("fetch product skus", err)
	}
	if len(skus) != len(skuIDs) {
		return out, ordermodel.ErrOrderItemNotFound.Terminal()
	}

	listSpu, err := h.base.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{
		Account: input.Account,
		ID:      lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID { return s.SpuID }),
	})
	if err != nil {
		return out, sharedmodel.WrapErr("fetch product spus", err)
	}

	skuMap := lo.KeyBy(skus, func(s catalogmodel.ProductSku) uuid.UUID { return s.ID })
	spuMap := lo.KeyBy(listSpu.Data, func(s catalogmodel.ProductSpu) uuid.UUID { return s.ID })

	// Step 2.5: FX snapshot.
	if len(listSpu.Data) == 0 {
		return out, ordermodel.ErrOrderItemNotFound.Terminal()
	}
	sellerCurrency := listSpu.Data[0].Currency
	for _, spu := range listSpu.Data {
		if spu.Currency != sellerCurrency {
			return out, ordermodel.ErrMixedCurrencyCart.Fmt(sellerCurrency, spu.Currency).Terminal()
		}
	}

	buyerCurrency, err := sharedcurrency.Infer(buyerProfile.Country)
	if err != nil {
		return out, sharedmodel.WrapErr("infer buyer currency", err)
	}

	var fxSnapshot commonmodel.ExchangeRateSnapshot
	if buyerCurrency != sellerCurrency {
		fxSnapshot, err = h.base.common.GetExchangeRates(ctx, commonbiz.GetExchangeRatesParams{})
		if err != nil {
			return out, sharedmodel.WrapErr("fx rate lookup", err)
		}
	}

	// Stored rate convention: amount_seller = amount_buyer * exchangeRate.
	exchangeRate := decimal.NewFromInt(1)
	if buyerCurrency != sellerCurrency {
		rateFrom := decimal.NewFromInt(1)
		if buyerCurrency != fxSnapshot.Base {
			r, ok := fxSnapshot.Rates[buyerCurrency]
			if !ok || r.Sign() <= 0 {
				return out, ordermodel.ErrFXRateUnavailable.Fmt(buyerCurrency).Terminal()
			}
			rateFrom = r
		}
		rateTo := decimal.NewFromInt(1)
		if sellerCurrency != fxSnapshot.Base {
			r, ok := fxSnapshot.Rates[sellerCurrency]
			if !ok || r.Sign() <= 0 {
				return out, ordermodel.ErrFXRateUnavailable.Fmt(sellerCurrency).Terminal()
			}
			rateTo = r
		}
		exchangeRate = rateTo.Div(rateFrom)
	}

	var exchangeRateNumeric pgtype.Numeric
	if err = exchangeRateNumeric.Scan(exchangeRate.String()); err != nil {
		return out, sharedmodel.WrapErr("encode exchange rate", err)
	}

	convertToBuyer := func(amount int64) int64 {
		if buyerCurrency == sellerCurrency {
			return amount
		}
		return commonbiz.ConvertAmountPure(amount, sellerCurrency, buyerCurrency, fxSnapshot.Rates)
	}

	// Step 3: Remove items from cart (skip on BuyNow). The compensator
	// re-inserts the (account, sku, qty) tuples with the original quantities
	// captured here — the DELETE has already happened by the time it runs.
	if !input.BuyNow {
		restoreAccountIDs := make([]uuid.UUID, len(input.Items))
		restoreSkuIDs := make([]uuid.UUID, len(input.Items))
		restoreQuantities := make([]int64, len(input.Items))
		for i, item := range input.Items {
			restoreAccountIDs[i] = input.Account.ID
			restoreSkuIDs[i] = item.SkuID
			restoreQuantities[i] = item.Quantity
		}
		saga.Defer("restore_cart", func(rctx restate.RunContext) error {
			return h.base.storage.Querier().RestoreCheckoutItems(rctx, orderdb.RestoreCheckoutItemsParams{
				AccountIds: restoreAccountIDs,
				SkuIds:     restoreSkuIDs,
				Quantities: restoreQuantities,
			})
		})
		if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
			_, e := h.base.storage.Querier().RemoveCheckoutItem(rctx, orderdb.RemoveCheckoutItemParams{
				AccountID: input.Account.ID,
				SkuID:     skuIDs,
			})
			return e
		}); err != nil {
			return out, sharedmodel.WrapErr("remove cart items", err)
		}
	}

	// Step 4: Reserve inventory.
	saga.Defer("release_inventory", func(rctx restate.RunContext) error {
		return h.base.inventory.ReleaseInventory(rctx, inventorybiz.ReleaseInventoryParams{
			Items: lo.Map(input.Items, func(item CheckoutItem, _ int) inventorybiz.ReleaseInventoryItem {
				return inventorybiz.ReleaseInventoryItem{
					RefType: inventorydb.InventoryStockRefTypeProductSku,
					RefID:   item.SkuID,
					Amount:  checkoutItemMap[item.SkuID].Quantity,
				}
			}),
		})
	})
	inventories, err := h.base.inventory.ReserveInventory(ctx, inventorybiz.ReserveInventoryParams{
		Items: lo.Map(input.Items, func(item CheckoutItem, _ int) inventorybiz.ReserveInventoryItem {
			var displayName string
			if sku, ok := skuMap[item.SkuID]; ok {
				if spu, ok := spuMap[sku.SpuID]; ok {
					displayName = spu.Name
				}
			}
			return inventorybiz.ReserveInventoryItem{
				RefType:     inventorydb.InventoryStockRefTypeProductSku,
				RefID:       item.SkuID,
				Amount:      checkoutItemMap[item.SkuID].Quantity,
				DisplayName: displayName,
			}
		}),
	})
	if err != nil {
		metrics.CheckoutItemsCreatedTotal.WithLabelValues("failure").Inc()
		return out, sharedmodel.WrapErr("reserve inventory", err)
	}

	serialIDsMap := lo.SliceToMap(inventories, func(i inventorybiz.ReserveInventoryResult) (uuid.UUID, []string) {
		return i.RefID, i.SerialIDs
	})

	// Step 5: Quote transport per item.
	sellerIDs := lo.Uniq(lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID {
		return spuMap[s.SpuID].AccountID
	}))

	sellerContacts, err := h.base.account.GetDefaultContact(ctx, sellerIDs)
	if err != nil {
		return out, sharedmodel.WrapErr("fetch seller contacts", err)
	}

	type transportQuote struct {
		Option string `json:"option"`
		Cost   int64  `json:"cost"`
	}
	transportQuotes := make(map[uuid.UUID]transportQuote)

	for _, item := range input.Items {
		sku := skuMap[item.SkuID]
		spu := spuMap[sku.SpuID]

		transportClient, tcErr := h.base.getTransportClient(item.TransportOption)
		if tcErr != nil {
			return out, sharedmodel.WrapErr("get transport client", tcErr)
		}

		sellerContact, ok := sellerContacts[spu.AccountID]
		if !ok {
			return out, sharedmodel.WrapErr("seller contact not found", ordermodel.ErrOrderItemNotFound)
		}

		quote, qErr := transportClient.Quote(ctx, transport.QuoteParams{
			Items: []transport.ItemMetadata{{
				SkuID:    item.SkuID,
				Quantity: item.Quantity,
			}},
			FromAddress: sellerContact.Address,
			ToAddress:   input.Address,
		})
		if qErr != nil {
			return out, sharedmodel.WrapErr(fmt.Sprintf("quote transport for sku %s", item.SkuID), qErr)
		}

		transportQuotes[item.SkuID] = transportQuote{
			Option: item.TransportOption,
			Cost:   quote.Cost,
		}
	}

	// Step 6: Compute totals.
	type itemAmounts struct {
		subtotalAmount int64
		totalAmount    int64
	}
	itemAmountsMap := make(map[uuid.UUID]itemAmounts)
	var total int64
	for _, item := range input.Items {
		sku := skuMap[item.SkuID]
		tq := transportQuotes[item.SkuID]
		subtotal := convertToBuyer(sku.Price * item.Quantity)
		paid := subtotal + convertToBuyer(tq.Cost)
		itemAmountsMap[item.SkuID] = itemAmounts{subtotalAmount: subtotal, totalAmount: paid}
		total += paid
	}

	// Step 7: Wallet / gateway split.
	var walletAmount, gatewayAmount int64
	if input.UseWallet && total > 0 {
		balance, balErr := h.base.account.GetWalletBalance(ctx, input.Account.ID)
		if balErr != nil {
			return out, sharedmodel.WrapErr("get wallet balance", balErr)
		}
		walletAmount = min(balance, total)
	}
	gatewayAmount = total - walletAmount

	if gatewayAmount > 0 && input.PaymentOption == "" {
		return out, ordermodel.ErrInsufficientWalletBalance.Terminal()
	}

	// Step 8: Atomically create payment_session, child txs, and items.
	type runResult struct {
		Session       orderdb.OrderPaymentSession `json:"session"`
		WalletTx      *orderdb.OrderTransaction   `json:"wallet_tx,omitempty"`
		GatewayTx     *orderdb.OrderTransaction   `json:"gateway_tx,omitempty"`
		CheckoutTxIDs []int64                     `json:"checkout_tx_ids"`
		BlockerTxID   int64                       `json:"blocker_tx_id"`
		Items         []orderdb.OrderItem         `json:"items"`
	}

	// The compensator runs against the session ID we capture below. It is
	// declared BEFORE the Run() so a panic mid-Run still triggers cleanup —
	// session/items/txs are all visible in DB even on partial commit because
	// each statement auto-commits in the absence of an outer tx (sqlc Queries
	// pool here). MarkPaymentSessionFailed and CancelItem are idempotent on
	// already-final rows. CreditFromSession is a cross-module call and is
	// invoked from the top-level defer (above), not from this RunContext.
	saga.Defer("mark_session_failed_and_cancel_items", func(rctx restate.RunContext) error {
		if sessionIDForCompensation == uuid.Nil {
			return nil
		}
		if _, e := h.base.storage.Querier().MarkPaymentSessionFailed(rctx, sessionIDForCompensation); e != nil {
			return e
		}
		items, e := h.base.storage.Querier().ListItemsByPaymentSession(rctx, sessionIDForCompensation)
		if e != nil {
			return e
		}
		for _, it := range items {
			if _, ce := h.base.storage.Querier().CancelItem(rctx, orderdb.CancelItemParams{
				ID:            it.ID,
				CancelledByID: uuid.NullUUID{},
			}); ce != nil {
				return ce
			}
		}
		return nil
	})

	created, err := restate.Run(ctx, func(rctx restate.RunContext) (runResult, error) {
		var res runResult

		session, sErr := h.base.storage.Querier().CreateDefaultPaymentSession(
			rctx,
			orderdb.CreateDefaultPaymentSessionParams{
				ID:          sessionID,
				Kind:        SessionKindBuyerCheckout,
				Status:      orderdb.OrderStatusPending,
				FromID:      uuid.NullUUID{UUID: input.Account.ID, Valid: true},
				ToID:        uuid.NullUUID{},
				Note:        "buyer checkout",
				Currency:    buyerCurrency,
				TotalAmount: total,
				Data:        json.RawMessage("{}"),
				DatePaid:    null.Time{},
				DateExpired: time.Now().Add(paymentExpiry),
			})
		if sErr != nil {
			return res, sharedmodel.WrapErr("db create payment session", sErr)
		}
		res.Session = session

		if walletAmount > 0 {
			tx, txErr := h.base.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     session.ID,
				Status:        orderdb.OrderStatusPending,
				Note:          "checkout wallet payment",
				Error:         null.String{},
				PaymentOption: null.String{},
				WalletID:      uuid.NullUUID{},
				Data:          json.RawMessage("{}"),
				Amount:        walletAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  exchangeRateNumeric,
				ReversesID:    null.Int{},
				DateSettled:   null.Time{},
				DateExpired:   null.Time{},
			})
			if txErr != nil {
				return res, sharedmodel.WrapErr("db create wallet tx", txErr)
			}
			res.WalletTx = &tx
			res.CheckoutTxIDs = append(res.CheckoutTxIDs, tx.ID)
			res.BlockerTxID = tx.ID
		}

		if gatewayAmount > 0 {
			tx, txErr := h.base.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     session.ID,
				Status:        orderdb.OrderStatusPending,
				Note:          "checkout gateway payment",
				Error:         null.String{},
				PaymentOption: null.StringFrom(input.PaymentOption),
				WalletID:      toNullUUID(input.WalletID),
				Data:          json.RawMessage("{}"),
				Amount:        gatewayAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  exchangeRateNumeric,
				ReversesID:    null.Int{},
				DateSettled:   null.Time{},
				DateExpired:   null.TimeFrom(time.Now().Add(paymentExpiry)),
			})
			if txErr != nil {
				return res, sharedmodel.WrapErr("db create gateway tx", txErr)
			}
			res.GatewayTx = &tx
			res.CheckoutTxIDs = append(res.CheckoutTxIDs, tx.ID)
			res.BlockerTxID = tx.ID
		}

		for _, checkoutItem := range input.Items {
			sku := skuMap[checkoutItem.SkuID]
			spu := spuMap[sku.SpuID]
			serialIDs := serialIDsMap[checkoutItem.SkuID]
			amounts := itemAmountsMap[checkoutItem.SkuID]
			tq := transportQuotes[checkoutItem.SkuID]

			jsonSerialIDs, mErr := sonic.Marshal(serialIDs)
			if mErr != nil {
				return res, sharedmodel.WrapErr("marshal serial ids", mErr)
			}

			skuName := spu.Name
			if len(sku.Attributes) > 0 {
				vals := make([]string, 0, len(sku.Attributes))
				for _, attr := range sku.Attributes {
					vals = append(vals, attr.Value)
				}
				skuName += " - " + strings.Join(vals, " / ")
			}

			dbItem, iErr := h.base.storage.Querier().CreateDefaultItem(rctx, orderdb.CreateDefaultItemParams{
				OrderID:          uuid.NullUUID{},
				AccountID:        input.Account.ID,
				SellerID:         spu.AccountID,
				SkuID:            sku.ID,
				SpuID:            sku.SpuID,
				SkuName:          skuName,
				Address:          input.Address,
				Note:             null.NewString(checkoutItem.Note, checkoutItem.Note != ""),
				SerialIds:        jsonSerialIDs,
				Quantity:         checkoutItem.Quantity,
				TransportOption:  tq.Option,
				SubtotalAmount:   amounts.subtotalAmount,
				TotalAmount:      amounts.totalAmount,
				PaymentSessionID: res.Session.ID,
				DateCancelled:    null.Time{},
				CancelledByID:    uuid.NullUUID{},
			})
			if iErr != nil {
				return res, sharedmodel.WrapErr("db create item", iErr)
			}

			res.Items = append(res.Items, dbItem)
		}

		return res, nil
	})
	if err != nil {
		metrics.CheckoutItemsCreatedTotal.WithLabelValues("failure").Inc()
		return out, sharedmodel.WrapErr("create txs and items", err)
	}
	metrics.CheckoutItemsCreatedTotal.WithLabelValues("success").Add(float64(len(created.Items)))
	sessionIDForCompensation = created.Session.ID

	// Step 9: Wallet debit + mark wallet tx success.
	if walletAmount > 0 && created.WalletTx != nil {
		walletTxID := created.WalletTx.ID
		saga.Defer("credit_wallet", func(rctx restate.RunContext) error {
			return h.base.account.WalletCredit(rctx, accountbiz.WalletCreditParams{
				AccountID: input.Account.ID,
				Amount:    walletAmount,
				Type:      "Refund",
				Reference: fmt.Sprintf("compensate-tx:%d", walletTxID),
				Note:      "checkout saga compensation",
			})
		})
		if _, dErr := h.base.account.WalletDebit(ctx, accountbiz.WalletDebitParams{
			AccountID: input.Account.ID,
			Amount:    walletAmount,
			Reference: fmt.Sprintf("tx:%d", walletTxID),
			Note:      "checkout wallet payment",
		}); dErr != nil {
			return out, sharedmodel.WrapErr("wallet debit", dErr)
		}
		if mErr := h.base.markTxSuccess(ctx, markTxSuccessParams{TxID: walletTxID}); mErr != nil {
			return out, sharedmodel.WrapErr("mark wallet tx success", mErr)
		}
	}

	// Step 10: Initiate gateway payment + publish payment_url
	url, gErr := h.base.InitGatewayPayment(ctx, InitGatewayPaymentParams{
		Amount:        gatewayAmount,
		PaymentOption: input.PaymentOption,
		BlockerTxID:   created.BlockerTxID,
		RefID:         workflowID.String(),
		Description:   fmt.Sprintf("Checkout session %s", workflowID),
	})
	if gErr != nil {
		return out, gErr
	}
	if pErr := restate.Promise[string](ctx, "payment_url").Resolve(url); pErr != nil {
		return out, sharedmodel.WrapErr("resolve payment url promise", pErr)
	}

	// Step 11: Wait for payment outcome, buyer cancel, or expiry. Wallet-only
	// checkouts short-circuit: the wallet leg is already Success, so we
	// resolve the event promise inline as "paid" before WaitFirst.
	//
	// `user_cancel` is a separate durable promise from `payment_event` so the
	// two sources stay disjoint: gateway/wallet outcomes flow through
	// ResolvePayment, buyer-initiated aborts flow through CancelCheckout. A
	// cancel resolved before Run() reaches WaitFirst is stored in the journal
	// and observed instantly when the race arrives.
	paymentPromise := restate.Promise[PaymentEvent](ctx, "payment_event")
	cancelPromise := restate.Promise[struct{}](ctx, "user_cancel")
	expiryFut := restate.After(ctx, paymentExpiry)

	if gatewayAmount == 0 {
		_ = paymentPromise.Resolve(PaymentEvent{Kind: "paid", TxID: created.BlockerTxID})
	}

	done, _ := restate.WaitFirst(ctx, paymentPromise, cancelPromise, expiryFut)
	switch done {
	case paymentPromise:
		ev, evErr := paymentPromise.Result()
		if evErr != nil {
			return out, sharedmodel.WrapErr("read payment event", evErr)
		}
		switch ev.Kind {
		case "paid":
			// fall through to success tail.
		case "failed":
			return out, ordermodel.ErrPaymentFailed.Terminal()
		default:
			return out, sharedmodel.WrapErr("unknown payment event kind", ordermodel.ErrPaymentFailed)
		}
	case cancelPromise:
		cancelled = true
		return CheckoutWorkflowOutput{Status: "cancelled", SessionID: sessionID}, nil
	case expiryFut:
		expired = true
		return CheckoutWorkflowOutput{Status: "expired", SessionID: sessionID}, nil
	}

	// Step 12: Success tail — clear saga, fan out side effects.
	saga.Clear()

	purchaseInteractions := make([]analyticbiz.CreateInteraction, 0, len(input.Items))
	for _, item := range input.Items {
		purchaseInteractions = append(purchaseInteractions, analyticbiz.CreateInteraction{
			Account:   input.Account,
			EventType: analyticmodel.EventPurchase,
			RefType:   analyticdb.AnalyticInteractionRefTypeProduct,
			RefID:     item.SkuID.String(),
		})
	}
	restate.ServiceSend(ctx, "Analytic", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
		Interactions: purchaseInteractions,
	})

	sellerItems := make(map[uuid.UUID][]string)
	for _, item := range input.Items {
		sku := skuMap[item.SkuID]
		spu := spuMap[sku.SpuID]
		sellerItems[spu.AccountID] = append(sellerItems[spu.AccountID], spu.Name)
	}
	for sellerID, names := range sellerItems {
		summary := ordermodel.SummarizeNames(names)
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: sellerID,
			Type:      accountmodel.NotiNewPendingItems,
			Channel:   accountmodel.ChannelInApp,
			Title:     "New pending items",
			Content:   fmt.Sprintf("New order for %s is waiting for your review.", summary),
		})
	}

	return CheckoutWorkflowOutput{Status: "paid", SessionID: sessionID}, nil
}

// WaitPaymentURL blocks the caller until Run() resolves the `payment_url`
// promise. Used by the HTTP transport to turn the async workflow submission
// into a sync response carrying the gateway redirect (or an empty string for
// wallet-only checkouts).
func (h *CheckoutWorkflowHandler) WaitPaymentURL(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) (string, error) {
	return restate.Promise[string](ctx, "payment_url").Result()
}

// ResolvePayment is the webhook entry point for the Restate side: callers (the
// payment webhook handler / wallet success path) invoke it to push a terminal
// PaymentEvent into the workflow's event promise, which Run() races against
// the expiry timer.
func (h *CheckoutWorkflowHandler) ResolvePayment(
	ctx restate.WorkflowSharedContext,
	ev PaymentEvent,
) error {
	return restate.Promise[PaymentEvent](ctx, "payment_event").Resolve(ev)
}

// CancelCheckout lets the buyer abort an in-flight checkout. It resolves the
// dedicated `user_cancel` promise (kept disjoint from `payment_event` so user
// actions don't share a channel with gateway/wallet outcomes), which Run()
// interprets as a terminal rollback (compensators run via the deferred saga).
func (h *CheckoutWorkflowHandler) CancelCheckout(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) error {
	return restate.Promise[struct{}](ctx, "user_cancel").Resolve(struct{}{})
}
