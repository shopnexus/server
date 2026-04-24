package orderbiz

import (
	"context"
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
	"shopnexus-server/internal/provider/payment/card"
	"shopnexus-server/internal/provider/payment/sepay"
	"shopnexus-server/internal/provider/payment/vnpay"
	"shopnexus-server/internal/provider/transport"
	sharedcurrency "shopnexus-server/internal/shared/currency"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"
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
		itemsCreated      bool
	)
	compensate := func() {
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
		if walletDeducted > 0 {
			if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
				AccountID: params.Account.ID,
				Amount:    walletDeducted,
				Type:      "Refund",
				Reference: fmt.Sprintf("checkout-compensate-%s", params.Account.ID),
				Note:      "checkout saga compensation",
			}); err != nil {
				slog.Error("saga compensate: wallet credit", slog.Any("error", err))
			}
		}
	}
	defer func() {
		if err != nil && (inventoryReserved || walletDeducted > 0) && !itemsCreated {
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

	exchangeRate := 1.0
	if buyerCurrency != sellerCurrency {
		rateFrom := 1.0
		if sellerCurrency != fxSnapshot.Base {
			r, ok := fxSnapshot.Rates[sellerCurrency]
			if !ok || r <= 0 {
				return zero, ordermodel.ErrFXRateUnavailable.Fmt(sellerCurrency).Terminal()
			}
			rateFrom = r
		}
		rateTo := 1.0
		if buyerCurrency != fxSnapshot.Base {
			r, ok := fxSnapshot.Rates[buyerCurrency]
			if !ok || r <= 0 {
				return zero, ordermodel.ErrFXRateUnavailable.Fmt(buyerCurrency).Terminal()
			}
			rateTo = r
		}
		exchangeRate = rateTo / rateFrom
	}

	var exchangeRateNumeric pgtype.Numeric
	if err := exchangeRateNumeric.Scan(fmt.Sprintf("%.10f", exchangeRate)); err != nil {
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
	// paid_amount = subtotal_amount + transport cost in buyer currency (per item).
	type itemAmounts struct {
		subtotalAmount int64
		paidAmount     int64
	}
	itemAmountsMap := make(map[uuid.UUID]itemAmounts)
	var total int64
	for _, item := range params.Items {
		sku := skuMap[item.SkuID]
		tq := transportQuotes[item.SkuID]
		subtotal := convertToBuyer(int64(sku.Price) * item.Quantity)
		paid := subtotal + convertToBuyer(tq.Cost)
		itemAmountsMap[item.SkuID] = itemAmounts{subtotalAmount: subtotal, paidAmount: paid}
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

	// Step 7: Create transactions and items atomically inside restate.Run.
	type runResult struct {
		WalletTx       *orderdb.OrderTransaction `json:"wallet_tx,omitempty"`
		GatewayTx      *orderdb.OrderTransaction `json:"gateway_tx,omitempty"`
		CheckoutTxIDs  []int64                   `json:"checkout_tx_ids"`
		BlockerTxID    int64                     `json:"blocker_tx_id"`
		Items          []orderdb.OrderItem       `json:"items"`
	}

	created, err := restate.Run(ctx, func(ctx restate.RunContext) (runResult, error) {
		var res runResult

		// Wallet tx (if any) — created first, status Success immediately.
		if walletAmount > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				FromID:        uuid.NullUUID{UUID: params.Account.ID, Valid: true},
				ToID:          uuid.NullUUID{},
				Type:          TxTypeCheckout,
				Status:        orderdb.OrderStatusSuccess,
				Note:          "checkout wallet payment",
				PaymentOption: null.String{},
				InstrumentID:  uuid.NullUUID{},
				Data:          json.RawMessage("{}"),
				Amount:        walletAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  exchangeRateNumeric,
				DatePaid:      null.TimeFrom(time.Now()),
				DateExpired:   time.Now(), // sentinel: already expired (instant)
			})
			if err != nil {
				return res, sharedmodel.WrapErr("db create wallet tx", err)
			}
			res.WalletTx = &tx
			res.CheckoutTxIDs = append(res.CheckoutTxIDs, tx.ID)
			res.BlockerTxID = tx.ID
		}

		// Gateway tx (if any) — status Pending until webhook confirms.
		if gatewayAmount > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				FromID:        uuid.NullUUID{UUID: params.Account.ID, Valid: true},
				ToID:          uuid.NullUUID{},
				Type:          TxTypeCheckout,
				Status:        orderdb.OrderStatusPending,
				Note:          "checkout gateway payment",
				PaymentOption: null.StringFrom(params.PaymentOption),
				InstrumentID:  toNullUUID(params.InstrumentID),
				Data:          json.RawMessage("{}"),
				Amount:        gatewayAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  exchangeRateNumeric,
				DatePaid:      null.Time{},
				DateExpired:   time.Now().Add(paymentExpiry),
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
				OrderID:       uuid.NullUUID{},
				AccountID:     params.Account.ID,
				SellerID:      spu.AccountID,
				SkuID:         sku.ID,
				SkuName:       skuName,
				Address:       params.Address,
				Note:          null.NewString(checkoutItem.Note, checkoutItem.Note != ""),
				SerialIds:     jsonSerialIDs,
				Quantity:      checkoutItem.Quantity,
				TransportOption: tq.Option,
				SubtotalAmount:  amounts.subtotalAmount,
				PaidAmount:    amounts.paidAmount,
				PaymentTxID:   res.BlockerTxID,
				DateCancelled: null.Time{},
				CancelledByID: uuid.NullUUID{},
				RefundTxID:    null.Int{},
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
	itemsCreated = true

	// Step 8: Debit wallet (outside Run — cross-module).
	if walletAmount > 0 {
		walletResult, err := b.account.WalletDebit(ctx, accountbiz.WalletDebitParams{
			AccountID: params.Account.ID,
			Amount:    walletAmount,
			Reference: fmt.Sprintf("tx:%d", created.BlockerTxID),
			Note:      "checkout wallet payment",
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("wallet debit", err)
		}
		walletDeducted = walletResult.Deducted
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
		}

		// Schedule timeout for gateway tx.
		restate.ServiceSend(ctx, b.ServiceName(), "TimeoutCheckoutTx").Send(
			TimeoutCheckoutTxParams{TxID: created.BlockerTxID},
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

	return b.enrichItems(ctx, dbItems)
}

// enrichItems converts DB items to model items (no separate resources enrichment needed here).
func (b *OrderHandler) enrichItems(ctx restate.Context, dbItems []orderdb.OrderItem) ([]ordermodel.OrderItem, error) {
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

	enriched, err := b.enrichItems(ctx, dbResult.Items)
	if err != nil {
		return zero, sharedmodel.WrapErr("enrich pending items", err)
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

	// Create refund tx and cancel item atomically.
	type cancelResult struct {
		RefundTx orderdb.OrderTransaction `json:"refund_tx"`
	}
	cancelRes, err := restate.Run(ctx, func(ctx restate.RunContext) (cancelResult, error) {
		// Create a refund tx (platform → buyer, Success immediately).
		// Only create if there is something to refund.
		var refundTx orderdb.OrderTransaction
		if item.PaidAmount > 0 {
			var txErr error
			refundTx, txErr = b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				FromID:        uuid.NullUUID{},
				ToID:          uuid.NullUUID{UUID: params.AccountID, Valid: true},
				Type:          TxTypeRefund,
				Status:        orderdb.OrderStatusSuccess,
				Note:          "buyer cancel pre-confirm",
				PaymentOption: null.String{},
				InstrumentID:  uuid.NullUUID{},
				Data:          json.RawMessage("{}"),
				Amount:        item.PaidAmount,
				FromCurrency:  buyerCurrency,
				ToCurrency:    buyerCurrency,
				ExchangeRate:  mustNumericOne(),
				DatePaid:      null.TimeFrom(time.Now()),
				DateExpired:   time.Now(),
			})
			if txErr != nil {
				return cancelResult{}, sharedmodel.WrapErr("db create refund tx", txErr)
			}
		}

		// Cancel item, linking refund tx.
		var refundTxID null.Int
		if refundTx.ID != 0 {
			refundTxID = null.IntFrom(refundTx.ID)
		}
		if _, err := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
			CancelledByID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
			RefundTxID:    refundTxID,
			ID:            params.ItemID,
		}); err != nil {
			return cancelResult{}, sharedmodel.WrapErr("db cancel item", err)
		}

		return cancelResult{RefundTx: refundTx}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("cancel item", err)
	}

	// Credit buyer wallet (outside Run — cross-module).
	if item.PaidAmount > 0 && cancelRes.RefundTx.ID != 0 {
		if err = b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
			AccountID: params.AccountID,
			Amount:    item.PaidAmount,
			Type:      "Refund",
			Reference: fmt.Sprintf("tx:%d", cancelRes.RefundTx.ID),
			Note:      "buyer cancel pre-confirm refund",
		}); err != nil {
			return sharedmodel.WrapErr("wallet credit", err)
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

// mustNumericOne returns a pgtype.Numeric with value 1 for identity-rate wallet txs.
func mustNumericOne() pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan("1")
	return n
}

// SetupPaymentMap initialises the payment client registry from config.
func (b *OrderHandler) SetupPaymentMap() error {
	var configs []sharedmodel.OptionConfig

	b.paymentMap = make(map[string]payment.Client)

	vnpayClients := vnpay.NewClients(vnpay.ClientOptions{
		TmnCode:    b.config.App.Vnpay.TmnCode,
		HashSecret: b.config.App.Vnpay.HashSecret,
		ReturnURL:  b.config.App.Vnpay.ReturnURL,
	})
	for _, c := range vnpayClients {
		b.paymentMap[c.Config().ID] = c
		configs = append(configs, c.Config())
	}

	sepayCfg := b.config.App.Sepay
	if sepayCfg.MerchantID != "" {
		sepayClient := sepay.NewClient(sepay.ClientOptions{
			MerchantID:   sepayCfg.MerchantID,
			SecretKey:    sepayCfg.SecretKey,
			IPNSecretKey: sepayCfg.IPNSecretKey,
			SuccessURL:   sepayCfg.SuccessURL,
			ErrorURL:     sepayCfg.ErrorURL,
			CancelURL:    sepayCfg.CancelURL,
			Sandbox:      sepayCfg.Sandbox,
		})
		b.paymentMap[sepayClient.Config().ID] = sepayClient
		configs = append(configs, sepayClient.Config())
	}

	cardCfg := b.config.App.CardPayment
	if cardCfg.Provider != "" {
		cardClient := card.NewClient(card.ClientOptions{
			Provider:  cardCfg.Provider,
			SecretKey: cardCfg.SecretKey,
			PublicKey: cardCfg.PublicKey,
		})
		b.paymentMap[cardClient.Config().ID] = cardClient
		configs = append(configs, cardClient.Config())
	}

	go func() {
		if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
			Category: "payment",
			Configs:  configs,
		}); err != nil {
			slog.Warn("register payment options: %v", slog.Any("error", err))
		}
	}()

	return nil
}

// getPaymentClient looks up a payment client by option ID.
func (b *OrderHandler) getPaymentClient(option string) (payment.Client, error) {
	client, ok := b.paymentMap[option]
	if !ok {
		return nil, ordermodel.ErrUnknownPaymentOption.Fmt(option).Terminal()
	}
	return client, nil
}

// getPaymentClientByProvider looks up a payment client by provider name.
func (b *OrderHandler) getPaymentClientByProvider(provider string) (payment.Client, error) {
	for _, client := range b.paymentMap {
		if client.Config().Provider == provider {
			return client, nil
		}
	}
	return nil, ordermodel.ErrUnknownPaymentOption.Fmt(provider).Terminal()
}
