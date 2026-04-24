package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/transport"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// ListSellerPendingItems returns paginated pending items for the seller.
func (b *OrderHandler) ListSellerPendingItems(
	ctx restate.Context,
	params ListSellerPendingItemsParams,
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	type incomingResult struct {
		Items []orderdb.OrderItem `json:"items"`
		Total int64               `json:"total"`
	}

	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (incomingResult, error) {
		items, err := b.storage.Querier().ListSellerPendingItems(ctx, params.SellerID)
		if err != nil {
			return incomingResult{}, err
		}

		total, err := b.storage.Querier().CountSellerPendingItems(ctx, params.SellerID)
		if err != nil {
			return incomingResult{}, err
		}

		return incomingResult{Items: items, Total: total}, nil
	})
	if err != nil {
		return zero, err
	}

	enriched, err := b.enrichItems(ctx, dbResult.Items)
	if err != nil {
		return zero, err
	}

	var totalVal null.Int64
	totalVal.SetValid(dbResult.Total)

	return sharedmodel.PaginateResult[ordermodel.OrderItem]{
		PageParams: params.PaginationParams,
		Total:      totalVal,
		Data:       enriched,
	}, nil
}

// ConfirmSellerPending groups selected pending items into an order with transport and fee transactions.
func (b *OrderHandler) ConfirmSellerPending(
	ctx restate.Context,
	params ConfirmSellerPendingParams,
) (_ ConfirmSellerPendingResult, err error) {
	defer metrics.TrackHandler("order", "ConfirmSellerPending", &err)()

	var zero ConfirmSellerPendingResult

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate confirm items", err)
	}

	sellerID := params.Account.ID

	// Step 1: Acquire seller lock.
	unlock := b.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", sellerID))
	defer unlock()

	// Step 2: Fetch and validate items.
	orderItems, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderItem, error) {
		items, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			ID: params.ItemIDs,
		})
		if err != nil {
			return nil, sharedmodel.WrapErr("db list items", err)
		}
		if len(items) != len(params.ItemIDs) {
			return nil, ordermodel.ErrOrderItemNotFound.Terminal()
		}
		return items, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch items", err)
	}

	// Validate items and aggregate shared fields.
	var buyerID uuid.UUID
	var address string
	var transportOption string
	var paidTotal int64
	uniquePaymentTxIDs := make(map[int64]struct{})

	for i, item := range orderItems {
		if item.OrderID.Valid {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyConfirmed)
		}
		if item.DateCancelled.Valid {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyCancelled)
		}
		if item.SellerID != sellerID {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemNotOwnedBySeller)
		}
		if i == 0 {
			buyerID = item.AccountID
			address = item.Address
			transportOption = item.TransportOption
		} else {
			if item.AccountID != buyerID {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameBuyer)
			}
			if item.Address != address {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameAddress)
			}
			if item.TransportOption != transportOption {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsTransportMismatch)
			}
		}
		paidTotal += item.PaidAmount
		uniquePaymentTxIDs[item.PaymentTxID] = struct{}{}
	}

	// Step 3: Verify every unique payment tx is Success.
	for txID := range uniquePaymentTxIDs {
		status, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderStatus, error) {
			tx, err := b.storage.Querier().GetTransaction(ctx, null.IntFrom(txID))
			if err != nil {
				return "", sharedmodel.WrapErr("get payment tx", err)
			}
			return tx.Status, nil
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("check payment tx status", err)
		}
		if status != orderdb.OrderStatusSuccess {
			return zero, ordermodel.ErrPaymentNotSuccess.Terminal()
		}
	}

	// Step 4: Get seller contact address for transport from_address.
	contactMap, err := b.account.GetDefaultContact(ctx, []uuid.UUID{sellerID})
	if err != nil {
		return zero, sharedmodel.WrapErr("get seller contact", err)
	}
	fromAddress := contactMap[sellerID].Address

	// Step 5: Get transport client and quote cost.
	transportClient, err := b.getTransportClient(transportOption)
	if err != nil {
		return zero, err
	}

	transportItems := lo.Map(orderItems, func(item orderdb.OrderItem, _ int) transport.ItemMetadata {
		return transport.ItemMetadata{
			SkuID:    item.SkuID,
			Quantity: item.Quantity,
		}
	})

	quote, err := transportClient.Quote(ctx, transport.QuoteParams{
		Items:       transportItems,
		FromAddress: fromAddress,
		ToAddress:   address,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("quote transport", err)
	}

	platformFee := int64(0) // TODO: plug config
	confirmFeeTotal := quote.Cost + platformFee

	// Infer seller currency for all transactions in this flow (outside Run — cross-module).
	sellerCurrency, err := b.inferCurrency(ctx, sellerID)
	if err != nil {
		return zero, sharedmodel.WrapErr("infer seller currency", err)
	}

	// Step 6: Wallet / gateway split for confirmFeeTotal.
	var confirmFeeWallet, confirmFeeGateway int64
	if params.UseWallet && confirmFeeTotal > 0 {
		balance, err := b.account.GetWalletBalance(ctx, sellerID)
		if err != nil {
			return zero, sharedmodel.WrapErr("get seller wallet balance", err)
		}
		if balance >= confirmFeeTotal {
			confirmFeeWallet = confirmFeeTotal
		} else {
			confirmFeeWallet = balance
		}
	}
	confirmFeeGateway = confirmFeeTotal - confirmFeeWallet

	if confirmFeeGateway > 0 && params.PaymentOption == "" {
		return zero, ordermodel.ErrInsufficientWalletBalance.Terminal()
	}

	// Step 7: Create transport, confirm_fee txs, payout tx, order, link items — all in one Run.
	type confirmRunResult struct {
		Transport       orderdb.OrderTransport   `json:"transport"`
		WalletTx        *orderdb.OrderTransaction `json:"wallet_tx,omitempty"`
		GatewayTx       *orderdb.OrderTransaction `json:"gateway_tx,omitempty"`
		PayoutTx        orderdb.OrderTransaction  `json:"payout_tx"`
		Order           orderdb.OrderOrder        `json:"order"`
		ConfirmFeeTxIDs []int64                   `json:"confirm_fee_tx_ids"`
		BlockerTxID     int64                     `json:"blocker_tx_id"`
	}

	runRes, err := restate.Run(ctx, func(ctx restate.RunContext) (confirmRunResult, error) {
		var res confirmRunResult

		// Create transport record.
		quoteData, _ := json.Marshal(map[string]int64{"quote": quote.Cost})
		trRow, err := b.storage.Querier().CreateDefaultTransport(ctx, orderdb.CreateDefaultTransportParams{
			Option: transportOption,
			Data:   json.RawMessage(quoteData),
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create transport", err)
		}
		res.Transport = trRow

		// Confirm_fee wallet tx (if any).
		if confirmFeeWallet > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				FromID:        uuid.NullUUID{UUID: sellerID, Valid: true},
				ToID:          uuid.NullUUID{},
				Type:          TxTypeConfirmFee,
				Status:        orderdb.OrderStatusSuccess,
				Note:          "confirm fee wallet payment",
				PaymentOption: null.String{},
				WalletID:  uuid.NullUUID{},
				Data:          json.RawMessage("{}"),
				Amount:        confirmFeeWallet,
				FromCurrency:  sellerCurrency,
				ToCurrency:    sellerCurrency,
				ExchangeRate:  mustNumericOne(),
				DatePaid:      null.TimeFrom(time.Now()),
				DateExpired:   time.Now(),
			})
			if err != nil {
				return res, sharedmodel.WrapErr("db create confirm_fee wallet tx", err)
			}
			res.WalletTx = &tx
			res.ConfirmFeeTxIDs = append(res.ConfirmFeeTxIDs, tx.ID)
			res.BlockerTxID = tx.ID
		}

		// Confirm_fee gateway tx (if any).
		if confirmFeeGateway > 0 {
			tx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
				FromID:        uuid.NullUUID{UUID: sellerID, Valid: true},
				ToID:          uuid.NullUUID{},
				Type:          TxTypeConfirmFee,
				Status:        orderdb.OrderStatusPending,
				Note:          "confirm fee gateway payment",
				PaymentOption: null.StringFrom(params.PaymentOption),
				WalletID:  toNullUUID(params.WalletID),
				Data:          json.RawMessage("{}"),
				Amount:        confirmFeeGateway,
				FromCurrency:  sellerCurrency,
				ToCurrency:    sellerCurrency,
				ExchangeRate:  mustNumericOne(),
				DatePaid:      null.Time{},
				DateExpired:   time.Now().Add(paymentExpiry),
			})
			if err != nil {
				return res, sharedmodel.WrapErr("db create confirm_fee gateway tx", err)
			}
			res.GatewayTx = &tx
			res.ConfirmFeeTxIDs = append(res.ConfirmFeeTxIDs, tx.ID)
			res.BlockerTxID = tx.ID // gateway wins as blocker
		}

		// Payout tx: platform → seller, Pending until escrow releases.
		payoutTx, err := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
			FromID:        uuid.NullUUID{},
			ToID:          uuid.NullUUID{UUID: sellerID, Valid: true},
			Type:          TxTypePayout,
			Status:        orderdb.OrderStatusPending,
			Note:          "seller payout (escrow)",
			PaymentOption: null.String{},
			WalletID:  uuid.NullUUID{},
			Data:          json.RawMessage("{}"),
			Amount:        paidTotal,
			FromCurrency:  sellerCurrency,
			ToCurrency:    sellerCurrency,
			ExchangeRate:  mustNumericOne(),
			DatePaid:      null.Time{},
			DateExpired:   time.Now().Add(365 * 24 * time.Hour), // far-future sentinel
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create payout tx", err)
		}
		res.PayoutTx = payoutTx

		// Create order (seller_tx_id = blocker confirm_fee tx).
		order, err := b.storage.Querier().CreateDefaultOrder(ctx, orderdb.CreateDefaultOrderParams{
			BuyerID:       buyerID,
			SellerID:      sellerID,
			TransportID:   trRow.ID,
			Address:       address,
			ConfirmedByID: params.Account.ID,
			SellerTxID:    res.BlockerTxID,
			Note:          null.NewString(params.Note, params.Note != ""),
		})
		if err != nil {
			return res, sharedmodel.WrapErr("db create order", err)
		}
		res.Order = order

		// Link items to order.
		if err = b.storage.Querier().SetItemsOrderID(ctx, orderdb.SetItemsOrderIDParams{
			OrderID: uuid.NullUUID{UUID: order.ID, Valid: true},
			ItemIds: params.ItemIDs,
		}); err != nil {
			return res, sharedmodel.WrapErr("db set items order id", err)
		}

		return res, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create order", err)
	}

	metrics.OrdersCreatedTotal.Inc()

	// Step 8: Debit seller wallet for confirm fee (outside Run — cross-module).
	if confirmFeeWallet > 0 {
		if _, err = b.account.WalletDebit(ctx, accountbiz.WalletDebitParams{
			AccountID: sellerID,
			Amount:    confirmFeeWallet,
			Reference: fmt.Sprintf("tx:%d", runRes.BlockerTxID),
			Note:      "confirm fee wallet payment",
		}); err != nil {
			return zero, sharedmodel.WrapErr("seller wallet debit", err)
		}
	}

	// Step 9: Initiate gateway payment for confirm fee (outside Run — cross-module + I/O).
	var gatewayURL *string
	if confirmFeeGateway > 0 {
		paymentClient, err := b.getPaymentClient(params.PaymentOption)
		if err != nil {
			return zero, sharedmodel.WrapErr("get payment client", err)
		}

		result, err := paymentClient.Create(ctx, payment.CreateParams{
			RefID:       runRes.BlockerTxID,
			Amount:      confirmFeeGateway,
			Description: fmt.Sprintf("Confirm fee tx %d", runRes.BlockerTxID),
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("create gateway payment for confirm fee", err)
		}
		if result.RedirectURL != "" {
			gatewayURL = &result.RedirectURL
		}

		// Schedule timeout for confirm_fee gateway tx.
		restate.ServiceSend(ctx, b.ServiceName(), "TimeoutConfirmFeeTx").Send(
			TimeoutConfirmFeeTxParams{
				TxID:    runRes.BlockerTxID,
				OrderID: runRes.Order.ID,
			},
			restate.WithDelay(paymentExpiry),
		)
	}

	// Notify buyer (fire-and-forget).
	itemNames := lo.Map(orderItems, func(it orderdb.OrderItem, _ int) string { return it.SkuName })
	summary := ordermodel.SummarizeNames(itemNames)
	notiMeta, _ := json.Marshal(map[string]string{"order_id": runRes.Order.ID.String()})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: buyerID,
		Type:      accountmodel.NotiItemsConfirmed,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Items confirmed",
		Content:   fmt.Sprintf("%s has been confirmed by the seller.", summary),
		Metadata:  notiMeta,
	})

	// Hydrate and return order.
	hydratedOrder, err := b.GetBuyerOrder(ctx, runRes.Order.ID)
	if err != nil {
		return zero, sharedmodel.WrapErr("get hydrated order", err)
	}

	return ConfirmSellerPendingResult{
		Order:                  hydratedOrder,
		ConfirmFeeTxIDs:        runRes.ConfirmFeeTxIDs,
		PayoutTxID:             runRes.PayoutTx.ID,
		BlockerTxID:            runRes.BlockerTxID,
		RequiresGatewayPayment: confirmFeeGateway > 0,
		GatewayURL:             gatewayURL,
	}, nil
}

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
		var totalRefund int64
		for _, item := range buyerItemList {
			totalRefund += item.PaidAmount
		}

		itemIDs := lo.Map(buyerItemList, func(it orderdb.OrderItem, _ int) int64 { return it.ID })

		// Infer buyer currency before the durable Run (outside Run — cross-module).
		buyerCurrency, err := b.inferCurrency(ctx, buyerID)
		if err != nil {
			return sharedmodel.WrapErr("infer buyer currency", err)
		}

		// Create refund tx and cancel each item atomically.
		type rejectResult struct {
			RefundTx orderdb.OrderTransaction `json:"refund_tx"`
		}

		rejRes, err := restate.Run(ctx, func(ctx restate.RunContext) (rejectResult, error) {
			var res rejectResult

			// Create refund tx (platform → buyer, Success immediately).
			if totalRefund > 0 {
				tx, txErr := b.storage.Querier().CreateDefaultTransaction(ctx, orderdb.CreateDefaultTransactionParams{
					FromID:        uuid.NullUUID{},
					ToID:          uuid.NullUUID{UUID: buyerID, Valid: true},
					Type:          TxTypeRefund,
					Status:        orderdb.OrderStatusSuccess,
					Note:          "seller reject pre-confirm",
					PaymentOption: null.String{},
					WalletID:  uuid.NullUUID{},
					Data:          json.RawMessage("{}"),
					Amount:        totalRefund,
					FromCurrency:  buyerCurrency,
					ToCurrency:    buyerCurrency,
					ExchangeRate:  mustNumericOne(),
					DatePaid:      null.TimeFrom(time.Now()),
					DateExpired:   time.Now(),
				})
				if txErr != nil {
					return res, sharedmodel.WrapErr("db create refund tx", txErr)
				}
				res.RefundTx = tx
			}

			// Cancel each item with seller as cancelled_by_id.
			for _, id := range itemIDs {
				var refundTxID null.Int
				if res.RefundTx.ID != 0 {
					refundTxID = null.IntFrom(res.RefundTx.ID)
				}
				if _, err := b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
					CancelledByID: uuid.NullUUID{UUID: sellerID, Valid: true},
					RefundTxID:    refundTxID,
					ID:            id,
				}); err != nil {
					return res, sharedmodel.WrapErr("db cancel item", err)
				}
			}

			return res, nil
		})
		if err != nil {
			return sharedmodel.WrapErr("reject items for buyer", err)
		}

		// Credit buyer wallet (outside Run — cross-module).
		if totalRefund > 0 && rejRes.RefundTx.ID != 0 {
			if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
				AccountID: buyerID,
				Amount:    totalRefund,
				Type:      "Refund",
				Reference: fmt.Sprintf("tx:%d", rejRes.RefundTx.ID),
				Note:      "seller reject pre-confirm refund",
			}); err != nil {
				return sharedmodel.WrapErr("wallet credit buyer", err)
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
