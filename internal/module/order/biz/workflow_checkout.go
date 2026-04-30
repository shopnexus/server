package orderbiz

import (
	"encoding/json"
	"fmt"
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
	"shopnexus-server/internal/provider/payment"
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

type CheckoutWorkflow struct {
	base      *OrderHandler
	storage   OrderStorage
	account   accountbiz.AccountBiz
	catalog   catalogbiz.CatalogBiz
	inventory inventorybiz.InventoryBiz
	common    commonbiz.CommonBiz
}

func (h *CheckoutWorkflow) Run(
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
	defer func() {
		if restate.IsTerminalError(err) {
			saga.Compensate()
		}
	}()

	// Step 1.5: Load buyer profile and enforce address country match.
	buyerProfile, err := h.account.GetProfile(ctx, accountbiz.GetProfileParams{
		Issuer:    input.Account,
		AccountID: input.Account.ID,
	})
	if err != nil {
		return out, sharedmodel.WrapErr("load buyer profile", err)
	}

	resolvedCountry, err := h.common.ResolveCountry(ctx, input.Address)
	if err != nil {
		return out, err
	}
	if resolvedCountry != buyerProfile.Country {
		return out, ordermodel.ErrCheckoutAddressCountryMismatch.Fmt(resolvedCountry, buyerProfile.Country).Terminal()
	}

	skuIDs := lo.Map(input.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	checkoutItemMap := lo.KeyBy(input.Items, func(s CheckoutItem) uuid.UUID { return s.SkuID })

	// Step 2: Fetch product data (SKUs + SPUs).
	skus, err := h.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return out, sharedmodel.WrapErr("fetch product skus", err)
	}
	if len(skus) != len(skuIDs) {
		return out, ordermodel.ErrOrderItemNotFound.Terminal()
	}

	listSpu, err := h.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{
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
		fxSnapshot, err = h.common.GetExchangeRates(ctx, commonbiz.GetExchangeRatesParams{})
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
			return h.storage.Querier().RestoreCheckoutItems(rctx, orderdb.RestoreCheckoutItemsParams{
				AccountIds: restoreAccountIDs,
				SkuIds:     restoreSkuIDs,
				Quantities: restoreQuantities,
			})
		})
		if err = restate.RunVoid(ctx, func(rctx restate.RunContext) error {
			_, e := h.storage.Querier().RemoveCheckoutItem(rctx, orderdb.RemoveCheckoutItemParams{
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
		return h.inventory.ReleaseInventory(rctx, inventorybiz.ReleaseInventoryParams{
			Items: lo.Map(input.Items, func(item CheckoutItem, _ int) inventorybiz.ReleaseInventoryItem {
				return inventorybiz.ReleaseInventoryItem{
					RefType: inventorydb.InventoryStockRefTypeProductSku,
					RefID:   item.SkuID,
					Amount:  checkoutItemMap[item.SkuID].Quantity,
				}
			}),
		})
	})
	inventories, err := h.inventory.ReserveInventory(ctx, inventorybiz.ReserveInventoryParams{
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

	sellerContacts, err := h.account.GetDefaultContact(ctx, sellerIDs)
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
	var internalWalletAmount, gatewayAmount int64
	if input.UseWallet && total > 0 {
		balance, balErr := h.base.GetWalletBalance(ctx, input.Account.ID)
		if balErr != nil {
			return out, sharedmodel.WrapErr("get wallet balance", balErr)
		}
		internalWalletAmount = min(balance, total)
	}
	gatewayAmount = total - internalWalletAmount

	if gatewayAmount > 0 && input.PaymentOption == "" {
		return out, ordermodel.ErrInsufficientWalletBalance.Terminal()
	}

	// Step 8: Atomically create payment_session, child txs (Pending), and
	// order items in a single restate.Run.
	internalWalletTxID := restate.UUID(ctx)
	gatewayTxID := restate.UUID(ctx)

	// Compensator: mark payment_session and every tx Failed. Both UPDATEs
	// guard on `status = 'Pending'`, so re-running on already-final rows is
	// a no-op (saga retries the compensator until it succeeds).
	txIDsToFail := make([]uuid.UUID, 0, 2)
	if internalWalletAmount > 0 {
		txIDsToFail = append(txIDsToFail, internalWalletTxID)
	}
	if gatewayAmount > 0 {
		txIDsToFail = append(txIDsToFail, gatewayTxID)
	}
	saga.Defer("mark_session_and_txs_failed", func(rctx restate.RunContext) error {
		return markSessionAndTxsFailed(rctx, h.storage.Querier(), sessionID, txIDsToFail, "checkout saga compensation")
	})

	// Result fields aren't needed downstream (the workflow drives the rest
	// from input + pre-allocated UUIDs), but keeping them in the journal
	// gives us a clean replay trace for debugging.
	type checkoutRunResult struct {
		Session orderdb.OrderPaymentSession `json:"session"`
		Items   []orderdb.OrderItem         `json:"items"`
	}
	_, err = restate.Run(ctx, func(rctx restate.RunContext) (checkoutRunResult, error) {
		var res checkoutRunResult

		session, sErr := h.storage.Querier().CreateDefaultPaymentSession(rctx, orderdb.CreateDefaultPaymentSessionParams{
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

		if internalWalletAmount > 0 {
			if _, txErr := h.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				ID:            internalWalletTxID,
				SessionID:     sessionID,
				Status:        orderdb.OrderStatusPending,
				Note:          "checkout wallet payment",
				Error:         null.String{},
				PaymentOption: null.String{},
				Data:          json.RawMessage("{}"),
				Amount:        internalWalletAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  exchangeRateNumeric,
				ReversesID:    uuid.NullUUID{},
				DateSettled:   null.Time{},
				DateExpired:   null.Time{},
			}); txErr != nil {
				return res, sharedmodel.WrapErr("db create wallet tx", txErr)
			}
		}

		if gatewayAmount > 0 {
			if _, txErr := h.storage.Querier().CreateDefaultTransaction(rctx, orderdb.CreateDefaultTransactionParams{
				ID:            gatewayTxID,
				SessionID:     sessionID,
				Status:        orderdb.OrderStatusPending,
				Note:          "checkout gateway payment",
				Error:         null.String{},
				PaymentOption: null.StringFrom(input.PaymentOption),
				Data:          json.RawMessage("{}"),
				Amount:        gatewayAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  exchangeRateNumeric,
				ReversesID:    uuid.NullUUID{},
				DateSettled:   null.Time{},
				DateExpired:   null.TimeFrom(time.Now().Add(paymentExpiry)),
			}); txErr != nil {
				return res, sharedmodel.WrapErr("db create gateway tx", txErr)
			}
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

			dbItem, iErr := h.storage.Querier().CreateDefaultItem(rctx, orderdb.CreateDefaultItemParams{
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
				PaymentSessionID: sessionID,
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
		return out, sharedmodel.WrapErr("create checkout records", err)
	}

	// Step 9: Internal wallet payment. The wallet tx was created Pending in
	// step 8; we mark it Success after the debit acknowledges.
	if internalWalletAmount > 0 {
		if _, dErr := restate.Run(ctx, func(rctx restate.RunContext) (WalletDebitResult, error) {
			return h.base.walletDebit(rctx, WalletDebitParams{
				AccountID: input.Account.ID,
				Amount:    internalWalletAmount,
				Reference: fmt.Sprintf("tx:%s", internalWalletTxID),
				Note:      "checkout internal wallet",
			})
		}); dErr != nil {
			return out, sharedmodel.WrapErr("debit internal wallet", dErr)
		}
		// Arm credit compensator AFTER debit confirmed. walletDebit uses a
		// DB tx that rolls back on any error → terminal failure means no
		// debit happened, so registering before would over-credit on saga
		// fire. The compensator goes straight to the storage layer because
		// saga.Compensate is already inside RunVoid; double-wrapping
		// (WalletCredit's own RunVoid) fails the RunContext type check.
		// TODO: xem lại step này, vì đang ko đc hỗ trợ idempotency => có thể double credit/debit
		saga.Defer("credit_internal_wallet", func(rctx restate.RunContext) error {
			_, e := h.storage.Querier().CreditInternalWallet(rctx, orderdb.CreditInternalWalletParams{
				ID:     input.Account.ID,
				Amount: internalWalletAmount,
			})
			return e
		})
		if mErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
			_, e := h.storage.Querier().MarkTransactionSuccess(rctx, orderdb.MarkTransactionSuccessParams{
				ID:          internalWalletTxID,
				DateSettled: time.Now(),
			})
			return e
		}); mErr != nil {
			return out, sharedmodel.WrapErr("mark wallet tx success", mErr)
		}
	}

	// Step 10: Initiate gateway payment + publish payment_url
	url, gErr := h.base.InitGatewayPayment(ctx, InitGatewayPaymentParams{
		TxID:          gatewayTxID,
		Amount:        gatewayAmount,
		PaymentOption: input.PaymentOption,
		Description:   fmt.Sprintf("Checkout session %s", workflowID),
	})
	if gErr != nil {
		return out, gErr
	}
	if pErr := restate.Promise[string](ctx, "payment_url").Resolve(url); pErr != nil {
		return out, sharedmodel.WrapErr("resolve payment url promise", pErr)
	}

	// Step 11: Wait for payment outcome, buyer cancel, or expiry
	paymentPromise := restate.Promise[payment.Notification](ctx, "payment_event")
	cancelPromise := restate.Promise[struct{}](ctx, "user_cancel")
	expiryFut := restate.After(ctx, paymentExpiry)

	// Wallet-only path: no gateway leg to wait on, short-circuit the promise
	// with the session UUID (RefID convention) so the wait below resolves
	// immediately on the success branch.
	if gatewayAmount == 0 {
		_ = paymentPromise.Resolve(payment.Notification{
			RefID:  sessionID.String(),
			Status: payment.StatusSuccess,
		})
	}

	done, _ := restate.WaitFirst(ctx, paymentPromise, cancelPromise, expiryFut)
	switch done {
	case paymentPromise:
		ev, evErr := paymentPromise.Result()
		if evErr != nil {
			return out, sharedmodel.WrapErr("read payment event", evErr)
		}
		switch ev.Status {
		case payment.StatusSuccess:
			// fall through to success tail.
		case payment.StatusFailed, payment.StatusExpired:
			return out, ordermodel.ErrPaymentFailed.Terminal()
		default:
			return out, sharedmodel.WrapErr("unknown payment event status", ordermodel.ErrPaymentFailed)
		}
	case cancelPromise:
		return CheckoutWorkflowOutput{Status: "cancelled", SessionID: sessionID}, nil
	case expiryFut:
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

// WaitPaymentURL blocks the caller until the `payment_url` promise resolves
func (h *CheckoutWorkflow) WaitPaymentURL(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) (string, error) {
	return restate.Promise[string](ctx, "payment_url").Result()
}

// PaymentNotification is called by the payment provider
func (h *CheckoutWorkflow) PaymentNotification(
	ctx restate.WorkflowSharedContext,
	ev payment.Notification,
) error {
	return restate.Promise[payment.Notification](ctx, "payment_event").Resolve(ev)
}

// CancelCheckout lets the buyer abort an in-flight checkout
func (h *CheckoutWorkflow) CancelCheckout(
	ctx restate.WorkflowSharedContext,
	_ struct{},
) error {
	return restate.Promise[struct{}](ctx, "user_cancel").Resolve(struct{}{})
}
