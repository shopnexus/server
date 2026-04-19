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
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/transport"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// BuyerCheckout creates pending items, processes payment (wallet + provider),
// and schedules timeout handlers. Flow: checkout -> pay -> seller confirms.
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

	skuIDs := lo.Map(params.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	checkoutItemMap := lo.KeyBy(params.Items, func(s CheckoutItem) uuid.UUID { return s.SkuID })

	// TODO: add idempotency key to prevent double-submit checkout

	// Saga compensation: track committed side effects and undo them on failure.
	// After compensation, the error is made terminal to prevent Restate retry
	// with stale journal entries (compensated state != journaled state).
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
				Note:      "Checkout failed after payment",
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

	// Step 3: Reserve inventory
	inventories, err := b.inventory.ReserveInventory(ctx, inventorybiz.ReserveInventoryParams{
		Items: lo.Map(params.Items, func(item CheckoutItem, _ int) inventorybiz.ReserveInventoryItem {
			return inventorybiz.ReserveInventoryItem{
				RefType: inventorydb.InventoryStockRefTypeProductSku,
				RefID:   item.SkuID,
				Amount:  checkoutItemMap[item.SkuID].Quantity,
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
	// Get seller addresses (from -> seller's default contact)
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

	// Step 5: Calculate total = sum(product costs) + sum(transport costs)
	var totalProductCost int64
	var totalTransportCost int64
	for _, item := range params.Items {
		sku := skuMap[item.SkuID]
		totalProductCost += int64(sku.Price) * item.Quantity
		totalTransportCost += transportQuotes[item.SkuID].Cost
	}
	total := totalProductCost + totalTransportCost

	// Step 6: Process payment
	var paymentID int64
	var redirectURL string
	walletOnly := false

	if params.UseWallet && total > 0 {
		walletResult, err := b.account.WalletDebit(ctx, accountbiz.WalletDebitParams{
			AccountID: params.Account.ID,
			Amount:    total,
			Reference: fmt.Sprintf("checkout-%s", params.Account.ID),
			Note:      "Checkout payment",
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("wallet debit", err)
		}
		walletDeducted = walletResult.Deducted
	}

	remaining := total - walletDeducted

	if remaining > 0 {
		// Create payment via provider
		paymentOption := params.PaymentOption
		if paymentOption == "" {
			paymentOption = "default"
		}

		paymentClient, err := b.getPaymentClient(paymentOption)
		if err != nil {
			return zero, err
		}

		type paymentResult struct {
			PaymentID   int64  `json:"payment_id"`
			RedirectURL string `json:"redirect_url"`
		}

		payInfo, err := restate.Run(ctx, func(ctx restate.RunContext) (paymentResult, error) {
			expiryDays := b.config.App.Order.PaymentExpiryDays
			if expiryDays <= 0 {
				expiryDays = 30
			}

			dbPayment, err := b.storage.Querier().CreateDefaultPayment(ctx, orderdb.CreateDefaultPaymentParams{
				AccountID:   params.Account.ID,
				Option:      paymentOption,
				Amount:      remaining,
				Data:        []byte("{}"),
				DateExpired: time.Now().Add(time.Hour * 24 * time.Duration(expiryDays)),
			})
			if err != nil {
				return paymentResult{}, sharedmodel.WrapErr("db create payment", err)
			}

			createdPayment, err := paymentClient.Create(ctx, payment.CreateParams{
				RefID:       dbPayment.ID,
				Amount:      remaining,
				Description: fmt.Sprintf("Payment %d", dbPayment.ID),
			})
			if err != nil {
				return paymentResult{}, sharedmodel.WrapErr("create payment order", err)
			}

			// Store redirect URL in payment data
			if createdPayment.RedirectURL != "" {
				data, _ := json.Marshal(map[string]string{"redirect_url": createdPayment.RedirectURL})
				_, _ = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
					ID:   dbPayment.ID,
					Data: data,
				})
			}

			return paymentResult{
				PaymentID:   dbPayment.ID,
				RedirectURL: createdPayment.RedirectURL,
			}, nil
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("create payment", err)
		}

		paymentID = payInfo.PaymentID
		redirectURL = payInfo.RedirectURL
	} else {
		// Wallet-only: create a payment record with status Success directly
		walletOnly = true

		type walletPayResult struct {
			PaymentID int64 `json:"payment_id"`
		}

		payInfo, err := restate.Run(ctx, func(ctx restate.RunContext) (walletPayResult, error) {
			expiryDays := b.config.App.Order.PaymentExpiryDays
			if expiryDays <= 0 {
				expiryDays = 30
			}

			dbPayment, err := b.storage.Querier().CreateDefaultPayment(ctx, orderdb.CreateDefaultPaymentParams{
				AccountID:   params.Account.ID,
				Option:      "wallet",
				Amount:      total,
				Data:        []byte("{}"),
				DatePaid:    null.TimeFrom(time.Now()),
				DateExpired: time.Now().Add(time.Hour * 24 * time.Duration(expiryDays)),
			})
			if err != nil {
				return walletPayResult{}, sharedmodel.WrapErr("db create wallet payment", err)
			}

			// Mark as Success immediately
			_, err = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
				ID: dbPayment.ID,
				Status: orderdb.NullOrderStatus{
					OrderStatus: orderdb.OrderStatusSuccess,
					Valid:       true,
				},
			})
			if err != nil {
				return walletPayResult{}, sharedmodel.WrapErr("db update payment status", err)
			}

			return walletPayResult{PaymentID: dbPayment.ID}, nil
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("create wallet payment", err)
		}

		paymentID = payInfo.PaymentID
	}

	// Step 7: Create items linked to payment_id
	type createdItemInfo struct {
		ID          int64  `json:"id"`
		SkuID       string `json:"sku_id"`
		DateCreated string `json:"date_created"`
	}
	createdItems, err := restate.Run(ctx, func(ctx restate.RunContext) ([]createdItemInfo, error) {
		var items []createdItemInfo
		for _, checkoutItem := range params.Items {
			sku := skuMap[checkoutItem.SkuID]
			spu := spuMap[sku.SpuID]
			serialIDs := serialIDsMap[checkoutItem.SkuID]
			tq := transportQuotes[checkoutItem.SkuID]

			jsonSerialIDs, err := sonic.Marshal(serialIDs)
			if err != nil {
				return nil, sharedmodel.WrapErr("marshal serial ids", err)
			}

			paidAmount := int64(sku.Price)*checkoutItem.Quantity + tq.Cost

			// Build display name: "SPU Name - Attr1 / Attr2"
			skuName := spu.Name
			if len(sku.Attributes) > 0 {
				vals := make([]string, 0, len(sku.Attributes))
				for _, attr := range sku.Attributes {
					vals = append(vals, attr.Value)
				}
				skuName += " - " + strings.Join(vals, " / ")
			}

			dbItem, err := b.storage.Querier().CreateItem(ctx, orderdb.CreateItemParams{
				AccountID:             params.Account.ID,
				SellerID:              spu.AccountID,
				Address:               params.Address,
				SkuID:                 sku.ID,
				SkuName:               skuName,
				Quantity:              checkoutItem.Quantity,
				UnitPrice:             int64(sku.Price),
				PaidAmount:            paidAmount,
				Note:                  null.NewString(checkoutItem.Note, checkoutItem.Note != ""),
				SerialIds:             jsonSerialIDs,
				TransportOption:       null.StringFrom(tq.Option),
				TransportCostEstimate: tq.Cost,
				PaymentID:             null.IntFrom(paymentID),
				DateCreated:           time.Now(),
				DateUpdated:           time.Now(),
			})
			if err != nil {
				return nil, sharedmodel.WrapErr("db create item", err)
			}

			items = append(items, createdItemInfo{
				ID:          dbItem.ID,
				SkuID:       dbItem.SkuID.String(),
				DateCreated: dbItem.DateCreated.Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		return items, nil
	})
	if err != nil {
		metrics.CheckoutItemsCreatedTotal.WithLabelValues("failure").Inc()
		return zero, sharedmodel.WrapErr("create items", err)
	}

	metrics.CheckoutItemsCreatedTotal.WithLabelValues("success").Add(float64(len(createdItems)))
	itemsCreated = true

	// Step 8: Post-checkout
	if !walletOnly {
		// Payment needs provider confirmation: schedule 15 min timeout
		restate.ServiceSend(ctx, "Order", "CancelUnpaidCheckout").Send(paymentID, restate.WithDelay(15*time.Minute))
	} else {
		// Wallet-only: schedule 48h seller timeout
		restate.ServiceSend(ctx, "Order", "AutoCancelPendingItems").Send(paymentID, restate.WithDelay(48*time.Hour))
	}

	// Remove from cart (skip if BuyNow)
	if !params.BuyNow {
		if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
			if _, err := b.storage.Querier().RemoveCheckoutItem(ctx, orderdb.RemoveCheckoutItemParams{
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

	// Track purchase interactions (fire-and-forget)
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

	// Notify sellers about new pending items (fire-and-forget)
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

	// Hydrate and return created items
	itemIDs := lo.Map(createdItems, func(info createdItemInfo, _ int) int64 { return info.ID })

	hydratedItems, err := b.hydrateItems(ctx, itemIDs)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate created items", err)
	}

	// Fetch payment for response
	var paymentModel *ordermodel.Payment
	dbPayments, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderPayment, error) {
		return b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
			ID: []int64{paymentID},
		})
	})
	if err == nil && len(dbPayments) > 0 {
		p := dbToPayment(dbPayments[0])
		paymentModel = &p
	}

	var redirectPtr *string
	if redirectURL != "" {
		redirectPtr = &redirectURL
	}

	return BuyerCheckoutResult{
		Items:          hydratedItems,
		Payment:        paymentModel,
		RedirectUrl:    redirectPtr,
		WalletDeducted: walletDeducted,
		Total:          total,
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

// enrichItems converts DB items to model items with resources.
func (b *OrderHandler) enrichItems(ctx restate.Context, dbItems []orderdb.OrderItem) ([]ordermodel.OrderItem, error) {
	if len(dbItems) == 0 {
		return []ordermodel.OrderItem{}, nil
	}

	// Lookup SKU -> SPU for product images
	skuIDs := lo.Uniq(lo.Map(dbItems, func(oi orderdb.OrderItem, _ int) uuid.UUID { return oi.SkuID }))

	skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return nil, err
	}
	skuToSpuMap := make(map[uuid.UUID]uuid.UUID, len(skus))
	for _, sku := range skus {
		skuToSpuMap[sku.ID] = sku.SpuID
	}

	spuIDs := lo.Uniq(lo.Values(skuToSpuMap))

	resourcesMap, err := b.common.GetResources(ctx, commonbiz.GetResourcesParams{
		RefType: commondb.CommonResourceRefTypeProductSpu,
		RefIDs:  spuIDs,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ordermodel.OrderItem, 0, len(dbItems))
	for _, oi := range dbItems {
		spuID := skuToSpuMap[oi.SkuID]

		var orderID *uuid.UUID
		if oi.OrderID.Valid {
			orderID = &oi.OrderID.UUID
		}
		var note *string
		if oi.Note.Valid {
			note = &oi.Note.String
		}

		var transportOption string
		if oi.TransportOption.Valid {
			transportOption = oi.TransportOption.String
		}

		var paymentIDPtr *int64
		if oi.PaymentID.Valid {
			v := oi.PaymentID.Int64
			paymentIDPtr = &v
		}

		var dateCancelled *time.Time
		if oi.DateCancelled.Valid {
			dateCancelled = &oi.DateCancelled.Time
		}

		result = append(result, ordermodel.OrderItem{
			ID:                    oi.ID,
			OrderID:               orderID,
			AccountID:             oi.AccountID,
			SellerID:              oi.SellerID,
			Address:               oi.Address,
			SkuID:                 oi.SkuID,
			SpuID:                 spuID,
			SkuName:               oi.SkuName,
			Quantity:              oi.Quantity,
			UnitPrice:             oi.UnitPrice,
			PaidAmount:            oi.PaidAmount,
			Note:                  note,
			SerialIds:             oi.SerialIds,
			TransportOption:       transportOption,
			TransportCostEstimate: oi.TransportCostEstimate,
			PaymentID:             paymentIDPtr,
			DateCancelled:         dateCancelled,
			DateCreated:           oi.DateCreated,
			Resources:             resourcesMap[spuID],
		})
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
		items, err := b.storage.Querier().ListBuyerPendingItems(ctx, orderdb.ListBuyerPendingItemsParams{
			AccountID: params.AccountID,
			Off:       params.Offset().Int32,
			Lim:       params.Limit.Int32,
		})
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

	var total null.Int64
	total.SetValid(dbResult.Total)

	return sharedmodel.PaginateResult[ordermodel.OrderItem]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       enriched,
	}, nil
}

// CancelBuyerPending cancels a pending item, releases inventory, and refunds wallet.
func (b *OrderHandler) CancelBuyerPending(ctx restate.Context, params CancelBuyerPendingParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate cancel pending item", err)
	}

	// Fetch the item
	type itemInfo struct {
		SkuID                 string `json:"sku_id"`
		SellerID              string `json:"seller_id"`
		Quantity              int64  `json:"quantity"`
		PaidAmount            int64  `json:"paid_amount"`
		TransportCostEstimate int64  `json:"transport_cost_estimate"`
	}
	info, err := restate.Run(ctx, func(ctx restate.RunContext) (itemInfo, error) {
		item, err := b.storage.Querier().GetItem(ctx, null.IntFrom(params.ItemID))
		if err != nil {
			return itemInfo{}, sharedmodel.WrapErr("db get item", err)
		}
		if item.AccountID != params.AccountID {
			return itemInfo{}, ordermodel.ErrOrderItemNotFound.Terminal()
		}
		// Check item is still pending: no order_id and not cancelled
		if item.OrderID.Valid {
			return itemInfo{}, ordermodel.ErrItemAlreadyConfirmed
		}
		if item.DateCancelled.Valid {
			return itemInfo{}, ordermodel.ErrItemAlreadyCancelled
		}
		return itemInfo{
			SkuID:                 item.SkuID.String(),
			SellerID:              item.SellerID.String(),
			Quantity:              item.Quantity,
			PaidAmount:            item.PaidAmount,
			TransportCostEstimate: item.TransportCostEstimate,
		}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch item", err)
	}

	skuID, _ := uuid.Parse(info.SkuID)

	// Release inventory
	if err := b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
		Items: []inventorybiz.ReleaseInventoryItem{{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   skuID,
			Amount:  info.Quantity,
		}},
	}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Cancel the item
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().CancelItemsByIDs(ctx, []int64{params.ItemID})
		return err
	}); err != nil {
		return sharedmodel.WrapErr("cancel item", err)
	}

	// Refund to wallet
	refundAmount := info.PaidAmount + info.TransportCostEstimate
	if refundAmount > 0 {
		if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
			AccountID: params.AccountID,
			Amount:    refundAmount,
			Type:      "Refund",
			Reference: fmt.Sprintf("cancel-item-%d", params.ItemID),
			Note:      "Refund for cancelled pending item",
		}); err != nil {
			return sharedmodel.WrapErr("wallet refund", err)
		}
	}

	// Notify seller (fire-and-forget)
	sellerUUID, _ := uuid.Parse(info.SellerID)
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: sellerUUID,
		Type:      accountmodel.NotiPendingItemCancelled,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Pending item cancelled",
		Content:   "A buyer has cancelled a pending item.",
	})

	return nil
}
