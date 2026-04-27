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
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/transport"
	sharedcurrency "shopnexus-server/internal/shared/currency"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// BuyerCheckout creates pending items and checkout transactions (wallet + optional gateway).
// Flow: checkout -> pay -> seller confirms.
func (b *OrderHandler) BuyerCheckout(
	ctx restate.Context,
	params BuyerCheckoutParams,
) (_ BuyerCheckoutResult, err error) {
	defer metrics.TrackHandler("order", "BuyerCheckout", &err)()

	var zero BuyerCheckoutResult

	// Step 1: Validate
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate checkout", err)
	}
	if params.BuyNow && len(params.Items) != 1 {
		return zero, ordermodel.ErrBuyNowSingleSkuOnly.Terminal()
	}

	// Step 1.5: Load buyer profile and enforce address country matches buyer profile country.
	buyerProfile, err := b.account.GetProfile(ctx, accountbiz.GetProfileParams{
		Issuer:    params.Account,
		AccountID: params.Account.ID,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("load buyer profile", err)
	}

	resolvedCountry, err := b.common.ResolveCountry(ctx, params.Address)
	if err != nil {
		return zero, err
	}
	if resolvedCountry != buyerProfile.Country {
		return zero, ordermodel.ErrCheckoutAddressCountryMismatch.Fmt(resolvedCountry, buyerProfile.Country).Terminal()
	}

	skuIDs := lo.Map(params.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	checkoutItemMap := lo.KeyBy(params.Items, func(s CheckoutItem) uuid.UUID { return s.SkuID })

	// Saga compensation: undo committed side effects on failure.
	var (
		inventoryReserved bool
		walletDeducted    int64
		sessionID         int64
	)
	// sessionID is set after Run() succeeds. Once set, compensate handles cleanup
	// session-aware: mark Failed, cancel items, refund settled portion via
	// CreditFromSession. Wallet legs are kept Pending until WalletDebit cross-module
	// ack flips them Success — so unsettled portions contribute 0 to the refund.
	compensate := func() {
		if sessionID != 0 {
			if _, mfErr := b.storage.Querier().MarkPaymentSessionFailed(ctx, sessionID); mfErr != nil {
				slog.Error("saga compensate: mark session failed", slog.Any("error", mfErr))
			}
			if items, lErr := b.storage.Querier().ListItemsByPaymentSession(ctx, sessionID); lErr == nil {
				for _, it := range items {
					if _, cErr := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
						ID:            it.ID,
						CancelledByID: uuid.NullUUID{},
					}); cErr != nil {
						slog.Error("saga compensate: cancel item", slog.Any("error", cErr))
					}
				}
			} else {
				slog.Error("saga compensate: list items by session", slog.Any("error", lErr))
			}
			if _, cfErr := b.CreditFromSession(ctx, CreditFromSessionParams{
				SessionID:  sessionID,
				AccountID:  params.Account.ID,
				CreditType: "Refund",
				Note:       "checkout saga compensation",
			}); cfErr != nil {
				slog.Error("saga compensate: credit from session", slog.Any("error", cfErr))
			}
		}
		if inventoryReserved {
			if err := b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
				Items: lo.Map(params.Items, func(item CheckoutItem, _ int) inventorybiz.ReleaseInventoryItem {
					return inventorybiz.ReleaseInventoryItem{
						RefType: inventorydb.InventoryStockRefTypeProductSku,
						RefID:   item.SkuID,
						Amount:  checkoutItemMap[item.SkuID].Quantity,
					}
				}),
			}); err != nil {
				slog.Error("saga compensate: release inventory", slog.Any("error", err))
			}
		}
	}
	defer func() {
		if err != nil && (inventoryReserved || sessionID != 0) {
			compensate()
			err = restate.TerminalError(err)
		}
	}()

	// Step 2: Fetch product data (SKUs + SPUs for seller_id and name)
	skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch product skus", err)
	}
	if len(skus) != len(skuIDs) {
		return zero, ordermodel.ErrOrderItemNotFound.Terminal()
	}

	listSpu, err := b.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{
		Account: params.Account,
		ID:      lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID { return s.SpuID }),
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch product spus", err)
	}

	skuMap := lo.KeyBy(skus, func(s catalogmodel.ProductSku) uuid.UUID { return s.ID })
	spuMap := lo.KeyBy(listSpu.Data, func(s catalogmodel.ProductSpu) uuid.UUID { return s.ID })

	// Step 2.5: Snapshot FX at checkout time.
	if len(listSpu.Data) == 0 {
		return zero, ordermodel.ErrOrderItemNotFound.Terminal()
	}
	sellerCurrency := listSpu.Data[0].Currency
	for _, spu := range listSpu.Data {
		if spu.Currency != sellerCurrency {
			return zero, ordermodel.ErrMixedCurrencyCart.Fmt(sellerCurrency, spu.Currency).Terminal()
		}
	}

	buyerCurrency, err := sharedcurrency.Infer(buyerProfile.Country)
	if err != nil {
		return zero, sharedmodel.WrapErr("infer buyer currency", err)
	}

	var fxSnapshot commonmodel.ExchangeRateSnapshot
	if buyerCurrency != sellerCurrency {
		fxSnapshot, err = b.common.GetExchangeRates(ctx, commonbiz.GetExchangeRatesParams{})
		if err != nil {
			return zero, sharedmodel.WrapErr("fx rate lookup", err)
		}
	}

	// Stored rate convention: FROM buyer TO seller — i.e.
	// amount_seller = amount_buyer * exchangeRate.
	// Math: with Rates[X] = "1 USD = X target", the multiplier from
	// currency A to B is Rates[B]/Rates[A]; here A=buyer, B=seller.
	exchangeRate := decimal.NewFromInt(1)
	if buyerCurrency != sellerCurrency {
		rateFrom := decimal.NewFromInt(1)
		if buyerCurrency != fxSnapshot.Base {
			r, ok := fxSnapshot.Rates[buyerCurrency]
			if !ok || r.Sign() <= 0 {
				return zero, ordermodel.ErrFXRateUnavailable.Fmt(buyerCurrency).Terminal()
			}
			rateFrom = r
		}
		rateTo := decimal.NewFromInt(1)
		if sellerCurrency != fxSnapshot.Base {
			r, ok := fxSnapshot.Rates[sellerCurrency]
			if !ok || r.Sign() <= 0 {
				return zero, ordermodel.ErrFXRateUnavailable.Fmt(sellerCurrency).Terminal()
			}
			rateTo = r
		}
		exchangeRate = rateTo.Div(rateFrom)
	}

	var exchangeRateNumeric pgtype.Numeric
	if err = exchangeRateNumeric.Scan(exchangeRate.String()); err != nil {
		return zero, sharedmodel.WrapErr("encode exchange rate", err)
	}

	convertToBuyer := func(amount int64) int64 {
		if buyerCurrency == sellerCurrency {
			return amount
		}
		return commonbiz.ConvertAmountPure(amount, sellerCurrency, buyerCurrency, fxSnapshot.Rates)
	}

	// Step 3: Reserve inventory
	inventories, err := b.inventory.ReserveInventory(ctx, inventorybiz.ReserveInventoryParams{
		Items: lo.Map(params.Items, func(item CheckoutItem, _ int) inventorybiz.ReserveInventoryItem {
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
		return zero, sharedmodel.WrapErr("reserve inventory", err)
	}

	inventoryReserved = true

	serialIDsMap := lo.SliceToMap(inventories, func(i inventorybiz.ReserveInventoryResult) (uuid.UUID, []string) {
		return i.RefID, i.SerialIDs
	})

	// Step 4: Quote transport per item individually
	sellerIDs := lo.Uniq(lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID {
		return spuMap[s.SpuID].AccountID
	}))

	sellerContacts, err := b.account.GetDefaultContact(ctx, sellerIDs)
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch seller contacts", err)
	}

	type transportQuote struct {
		Option string `json:"option"`
		Cost   int64  `json:"cost"`
	}
	transportQuotes := make(map[uuid.UUID]transportQuote) // skuID -> quote

	for _, item := range params.Items {
		sku := skuMap[item.SkuID]
		spu := spuMap[sku.SpuID]

		transportClient, err := b.getTransportClient(item.TransportOption)
		if err != nil {
			return zero, sharedmodel.WrapErr("get transport client", err)
		}

		sellerContact, ok := sellerContacts[spu.AccountID]
		if !ok {
			return zero, sharedmodel.WrapErr("seller contact not found", ordermodel.ErrOrderItemNotFound)
		}

		quote, err := transportClient.Quote(ctx, transport.QuoteParams{
			Items: []transport.ItemMetadata{{
				SkuID:    item.SkuID,
				Quantity: item.Quantity,
			}},
			FromAddress: sellerContact.Address,
			ToAddress:   params.Address,
		})
		if err != nil {
			return zero, sharedmodel.WrapErr(fmt.Sprintf("quote transport for sku %s", item.SkuID), err)
		}

		transportQuotes[item.SkuID] = transportQuote{
			Option: item.TransportOption,
			Cost:   quote.Cost,
		}
	}

	// Step 5: Calculate totals.
	// subtotal_amount = product cost in buyer currency (per item).
	// total_amount = subtotal_amount + transport cost in buyer currency (per item).
	type itemAmounts struct {
		subtotalAmount int64
		totalAmount    int64
	}
	itemAmountsMap := make(map[uuid.UUID]itemAmounts)
	var total int64
	for _, item := range params.Items {
		sku := skuMap[item.SkuID]
		tq := transportQuotes[item.SkuID]
		subtotal := convertToBuyer(int64(sku.Price) * item.Quantity)
		paid := subtotal + convertToBuyer(tq.Cost)
		itemAmountsMap[item.SkuID] = itemAmounts{subtotalAmount: subtotal, totalAmount: paid}
		total += paid
	}

	// Step 6: Determine wallet / gateway split.
	var walletAmount, gatewayAmount int64
	if params.UseWallet && total > 0 {
		balance, err := b.account.GetWalletBalance(ctx, params.Account.ID)
		if err != nil {
			return zero, sharedmodel.WrapErr("get wallet balance", err)
		}
		if balance >= total {
			walletAmount = total
		} else {
			walletAmount = balance
		}
	}
	gatewayAmount = total - walletAmount

	if gatewayAmount > 0 && params.PaymentOption == "" {
		return zero, ordermodel.ErrInsufficientWalletBalance.Terminal()
	}

	// Step 7: Create payment_session, child txs, and items atomically inside restate.Run.
	// One session per checkout (kind=buyer-checkout), 1-2 child txs (wallet + gateway split),
	// and N items pointing at the session.
	type runResult struct {
		Session       orderdb.OrderPaymentSession `json:"session"`
		WalletTx      *orderdb.OrderTransaction   `json:"wallet_tx,omitempty"`
		GatewayTx     *orderdb.OrderTransaction   `json:"gateway_tx,omitempty"`
		CheckoutTxIDs []int64                     `json:"checkout_tx_ids"`
		BlockerTxID   int64                       `json:"blocker_tx_id"`
		Items         []orderdb.OrderItem         `json:"items"`
	}

	created, err := restate.Run(ctx, func(ctx restate.RunContext) (runResult, error) {
		var res runResult

		// Session is created Pending unconditionally. It is auto-promoted to Success
		// by MarkTxSuccess once every child tx settles (wallet tx after WalletDebit
		// ack; gateway tx after webhook). This keeps order DB and account DB
		// consistent across the saga split.
		session, err := b.storage.Querier().CreateDefaultPaymentSession(ctx, orderdb.CreateDefaultPaymentSessionParams{
			Kind:        SessionKindBuyerCheckout,
			Status:      orderdb.OrderStatusPending,
			FromID:      uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			ToID:        uuid.NullUUID{},
			Note:        "buyer checkout",
			Currency:    buyerCurrency,
			TotalAmount: total,
			Data:        json.RawMessage("{}"),
			DatePaid:    null.Time{},
			DateExpired: time.Now().Add(paymentExpiry),
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create payment session", err)
		}
		res.Session = session

		// Wallet tx (if any) — Pending until WalletDebit cross-module ack flips it
		// Success. Avoids minting balance via CreditFromSession on saga rollback.
		if walletAmount > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
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
			if err != nil {
				return res, sharedmodel.WrapErr("db create wallet tx", err)
			}
			res.WalletTx = &tx
			res.CheckoutTxIDs = append(res.CheckoutTxIDs, tx.ID)
			res.BlockerTxID = tx.ID
		}

		// Gateway tx (if any) — Pending until webhook confirms.
		if gatewayAmount > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				SessionID:     session.ID,
				Status:        orderdb.OrderStatusPending,
				Note:          "checkout gateway payment",
				Error:         null.String{},
				PaymentOption: null.StringFrom(params.PaymentOption),
				WalletID:      toNullUUID(params.WalletID),
				Data:          json.RawMessage("{}"),
				Amount:        gatewayAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  exchangeRateNumeric,
				ReversesID:    null.Int{},
				DateSettled:   null.Time{},
				DateExpired:   null.TimeFrom(time.Now().Add(paymentExpiry)),
			})
			if err != nil {
				return res, sharedmodel.WrapErr("db create gateway tx", err)
			}
			res.GatewayTx = &tx
			res.CheckoutTxIDs = append(res.CheckoutTxIDs, tx.ID)
			res.BlockerTxID = tx.ID // gateway wins as blocker
		}

		// Create items linked to blocker tx.
		for _, checkoutItem := range params.Items {
			sku := skuMap[checkoutItem.SkuID]
			spu := spuMap[sku.SpuID]
			serialIDs := serialIDsMap[checkoutItem.SkuID]
			amounts := itemAmountsMap[checkoutItem.SkuID]
			tq := transportQuotes[checkoutItem.SkuID]

			jsonSerialIDs, err := sonic.Marshal(serialIDs)
			if err != nil {
				return res, sharedmodel.WrapErr("marshal serial ids", err)
			}

			// Build display name: "SPU Name - Attr1 / Attr2"
			skuName := spu.Name
			if len(sku.Attributes) > 0 {
				vals := make([]string, 0, len(sku.Attributes))
				for _, attr := range sku.Attributes {
					vals = append(vals, attr.Value)
				}
				skuName += " - " + strings.Join(vals, " / ")
			}

			dbItem, err := b.storage.Querier().CreateDefaultItem(ctx, orderdb.CreateDefaultItemParams{
				OrderID:          uuid.NullUUID{},
				AccountID:        params.Account.ID,
				SellerID:         spu.AccountID,
				SkuID:            sku.ID,
				SpuID:            sku.SpuID,
				SkuName:          skuName,
				Address:          params.Address,
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
			if err != nil {
				return res, sharedmodel.WrapErr("db create item", err)
			}

			res.Items = append(res.Items, dbItem)
		}

		return res, nil
	})
	if err != nil {
		metrics.CheckoutItemsCreatedTotal.WithLabelValues("failure").Inc()
		return zero, sharedmodel.WrapErr("create txs and items", err)
	}

	metrics.CheckoutItemsCreatedTotal.WithLabelValues("success").Add(float64(len(created.Items)))
	sessionID = created.Session.ID

	// Step 8: Debit wallet (outside Run — cross-module). On success, mark the
	// wallet tx Success so it counts toward CreditFromSession sums going forward;
	// MarkTxSuccess auto-promotes the session if there's no gateway leg.
	if walletAmount > 0 && created.WalletTx != nil {
		walletResult, err := b.account.WalletDebit(ctx, accountbiz.WalletDebitParams{
			AccountID: params.Account.ID,
			Amount:    walletAmount,
			Reference: fmt.Sprintf("tx:%d", created.WalletTx.ID),
			Note:      "checkout wallet payment",
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("wallet debit", err)
		}
		walletDeducted = walletResult.Deducted
		if err := b.MarkTxSuccess(ctx, MarkTxSuccessParams{TxID: created.WalletTx.ID}); err != nil {
			return zero, sharedmodel.WrapErr("mark wallet tx success", err)
		}
	}

	// Step 9: Initiate gateway payment (outside Run — cross-module + I/O).
	var gatewayURL *string
	if gatewayAmount > 0 {
		paymentClient, err := b.getPaymentClient(params.PaymentOption)
		if err != nil {
			return zero, sharedmodel.WrapErr("get payment client", err)
		}

		result, err := paymentClient.Create(ctx, payment.CreateParams{
			RefID:       created.BlockerTxID,
			Amount:      gatewayAmount,
			Description: fmt.Sprintf("Checkout tx %d", created.BlockerTxID),
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("create gateway payment", err)
		}
		if result.RedirectURL != "" {
			gatewayURL = &result.RedirectURL

			// Persist the URL on tx.data so the buyer can resume payment from the
			// pending list ("Continue Payment") even after the checkout response
			// is gone from memory.
			data, _ := json.Marshal(map[string]string{"gateway_url": result.RedirectURL})
			if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
				return b.storage.Querier().SetTransactionData(ctx, orderdb.SetTransactionDataParams{
					ID:   created.BlockerTxID,
					Data: data,
				})
			}); err != nil {
				return zero, sharedmodel.WrapErr("persist gateway url on tx", err)
			}
		}

		// Schedule timeout for the checkout session (covers both the gateway URL window
		// and any orphan Pending state if the webhook never lands).
		restate.ServiceSend(ctx, b.ServiceName(), "TimeoutCheckoutSession").Send(
			TimeoutCheckoutSessionParams{SessionID: created.Session.ID},
			restate.WithDelay(paymentExpiry),
		)
	}

	// Step 10: Remove from cart (skip if BuyNow).
	if !params.BuyNow {
		if err = restate.RunVoid(ctx, func(ctx restate.RunContext) error {
			if _, err = b.storage.Querier().RemoveCheckoutItem(ctx, orderdb.RemoveCheckoutItemParams{
				AccountID: params.Account.ID,
				SkuID:     skuIDs,
			}); err != nil {
				return sharedmodel.WrapErr("db remove checkout items", err)
			}
			return nil
		}); err != nil {
			return zero, sharedmodel.WrapErr("remove cart items", err)
		}
	}

	// Track purchase interactions (fire-and-forget).
	var purchaseInteractions []analyticbiz.CreateInteraction
	for _, item := range params.Items {
		purchaseInteractions = append(purchaseInteractions, analyticbiz.CreateInteraction{
			Account:   params.Account,
			EventType: analyticmodel.EventPurchase,
			RefType:   analyticdb.AnalyticInteractionRefTypeProduct,
			RefID:     item.SkuID.String(),
		})
	}
	restate.ServiceSend(ctx, "Analytic", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
		Interactions: purchaseInteractions,
	})

	// Notify sellers about new pending items (fire-and-forget).
	sellerItems := make(map[uuid.UUID][]string)
	for _, item := range params.Items {
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

	// Hydrate and return created items.
	itemIDs := lo.Map(created.Items, func(it orderdb.OrderItem, _ int) int64 { return it.ID })
	hydratedItems, err := b.hydrateItems(ctx, itemIDs)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate created items", err)
	}

	return BuyerCheckoutResult{
		Items:                  hydratedItems,
		CheckoutTxIDs:          created.CheckoutTxIDs,
		BlockerTxID:            created.BlockerTxID,
		RequiresGatewayPayment: gatewayAmount > 0,
		GatewayURL:             gatewayURL,
		WalletDeducted:         walletDeducted,
		Total:                  total,
	}, nil
}

// mustNumericOne returns a pgtype.Numeric with value 1 for identity-rate wallet txs.
func mustNumericOne() pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan("1")
	return n
}
