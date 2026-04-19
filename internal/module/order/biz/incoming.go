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
		items, err := b.storage.Querier().ListSellerPendingItems(ctx, orderdb.ListSellerPendingItemsParams{
			SellerID: params.SellerID,
			Off:      params.Offset().Int32,
			Lim:      params.Limit.Int32,
		})
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

	var total null.Int64
	total.SetValid(dbResult.Total)

	return sharedmodel.PaginateResult[ordermodel.OrderItem]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       enriched,
	}, nil
}

// ConfirmSellerPending groups selected pending items into an order with transport.
func (b *OrderHandler) ConfirmSellerPending(
	ctx restate.Context,
	params ConfirmSellerPendingParams,
) (_ ordermodel.Order, err error) {
	defer metrics.TrackHandler("order", "ConfirmSellerPending", &err)()

	var zero ordermodel.Order

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate confirm items", err)
	}

	// Lock: exclusive — blocks concurrent reads and other writes on this seller's pending items
	unlock := b.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", params.Account.ID))
	defer unlock()

	// Step 1: Fetch items and validate
	type fetchedItem struct {
		ID                    int64  `json:"id"`
		AccountID             string `json:"account_id"`
		SellerID              string `json:"seller_id"`
		Address               string `json:"address"`
		SkuID                 string `json:"sku_id"`
		SkuName               string `json:"sku_name"`
		Quantity              int64  `json:"quantity"`
		UnitPrice             int64  `json:"unit_price"`
		PaidAmount            int64  `json:"paid_amount"`
		TransportOption       string `json:"transport_option"`
		TransportCostEstimate int64  `json:"transport_cost_estimate"`
		OrderIDValid          bool   `json:"order_id_valid"`
		DateCancelledValid    bool   `json:"date_cancelled_valid"`
		PaymentID             int64  `json:"payment_id"`
		PaymentIDValid        bool   `json:"payment_id_valid"`
	}
	type fetchResult struct {
		Items []fetchedItem `json:"items"`
	}

	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (fetchResult, error) {
		items, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			ID: params.ItemIDs,
		})
		if err != nil {
			return fetchResult{}, sharedmodel.WrapErr("db list items", err)
		}
		if len(items) != len(params.ItemIDs) {
			return fetchResult{}, ordermodel.ErrOrderItemNotFound.Terminal()
		}

		result := make([]fetchedItem, 0, len(items))
		for _, item := range items {
			result = append(result, fetchedItem{
				ID:                    item.ID,
				AccountID:             item.AccountID.String(),
				SellerID:              item.SellerID.String(),
				Address:               item.Address,
				SkuID:                 item.SkuID.String(),
				SkuName:               item.SkuName,
				Quantity:              item.Quantity,
				UnitPrice:             item.UnitPrice,
				PaidAmount:            item.PaidAmount,
				TransportOption:       item.TransportOption.String,
				TransportCostEstimate: item.TransportCostEstimate,
				OrderIDValid:          item.OrderID.Valid,
				DateCancelledValid:    item.DateCancelled.Valid,
				PaymentID:             item.PaymentID.Int64,
				PaymentIDValid:        item.PaymentID.Valid,
			})
		}
		return fetchResult{Items: result}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch items", err)
	}

	// Verify that items' payment status is Success before confirming
	if len(fetched.Items) > 0 && fetched.Items[0].PaymentIDValid {
		type paymentCheck struct {
			Status string `json:"status"`
		}
		pc, err := restate.Run(ctx, func(ctx restate.RunContext) (paymentCheck, error) {
			p, err := b.storage.Querier().GetPayment(ctx, null.IntFrom(fetched.Items[0].PaymentID))
			if err != nil {
				return paymentCheck{}, sharedmodel.WrapErr("get payment", err)
			}
			return paymentCheck{Status: string(p.Status)}, nil
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("check payment status", err)
		}
		if pc.Status != string(orderdb.OrderStatusSuccess) {
			return zero, ordermodel.ErrPaymentNotSuccess.Terminal()
		}
	}

	// Validate all items: not yet in an order, not cancelled, same seller, same buyer, same transport option
	sellerID := params.Account.ID
	var buyerID uuid.UUID
	var address string
	var transportOption string
	for i, item := range fetched.Items {
		if item.OrderIDValid {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyConfirmed)
		}
		if item.DateCancelledValid {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemAlreadyCancelled)
		}
		itemSellerID, _ := uuid.Parse(item.SellerID)
		if itemSellerID != sellerID {
			return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemNotOwnedBySeller)
		}
		itemBuyerID, _ := uuid.Parse(item.AccountID)
		if i == 0 {
			buyerID = itemBuyerID
			address = item.Address
			transportOption = item.TransportOption
		} else {
			if itemBuyerID != buyerID {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameBuyer)
			}
			if item.Address != address {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsNotSameAddress)
			}
			if item.TransportOption != transportOption {
				return zero, fmt.Errorf("item %d: %w", item.ID, ordermodel.ErrItemsTransportMismatch)
			}
		}
	}

	// Step 2: Get seller's default contact address (for transport from_address)
	contactMap, err := b.account.GetDefaultContact(ctx, []uuid.UUID{sellerID})
	if err != nil {
		return zero, sharedmodel.WrapErr("get seller contact", err)
	}
	fromAddress := contactMap[sellerID].Address

	// Step 3: Get transport client and create transport
	transportClient, err := b.getTransportClient(transportOption)
	if err != nil {
		return zero, err
	}

	// Build item metadata for transport
	transportItems := lo.Map(fetched.Items, func(item fetchedItem, _ int) transport.ItemMetadata {
		skuID, _ := uuid.Parse(item.SkuID)
		return transport.ItemMetadata{
			SkuID:    skuID,
			Quantity: item.Quantity,
		}
	})

	type transportResult struct {
		TransportID string `json:"transport_id"`
		Cost        int64  `json:"cost"`
	}

	tResult, err := restate.Run(ctx, func(ctx restate.RunContext) (transportResult, error) {
		created, err := transportClient.Create(ctx, transport.CreateParams{
			Items:       transportItems,
			FromAddress: fromAddress,
			ToAddress:   address,
			Option:      transportOption,
		})
		if err != nil {
			return transportResult{}, sharedmodel.WrapErr("create transport", err)
		}

		// Store transport in DB
		dbTransport, err := b.storage.Querier().CreateTransport(ctx, orderdb.CreateTransportParams{
			ID:     created.ID,
			Option: transportOption,
			Cost:   created.Cost,
			Data:   created.Data,
		})
		if err != nil {
			return transportResult{}, sharedmodel.WrapErr("db save transport", err)
		}

		return transportResult{
			TransportID: dbTransport.ID.String(),
			Cost:        dbTransport.Cost,
		}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create transport", err)
	}

	transportID, _ := uuid.Parse(tResult.TransportID)

	// Step 4: Calculate costs (buyer already paid full at checkout, so discount is 0)
	var productCost int64
	for _, item := range fetched.Items {
		productCost += item.UnitPrice * item.Quantity
	}
	transportCost := tResult.Cost
	total := productCost + transportCost

	// Step 5: Create order and set items' order_id
	type orderResult struct {
		OrderID string `json:"order_id"`
	}
	oResult, err := restate.Run(ctx, func(ctx restate.RunContext) (orderResult, error) {
		order, err := b.storage.Querier().CreateOrder(ctx, orderdb.CreateOrderParams{
			ID:              uuid.Must(uuid.NewRandom()),
			BuyerID:         buyerID,
			SellerID:        sellerID,
			TransportID:     uuid.NullUUID{UUID: transportID, Valid: true},
			ConfirmedByID:   uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			Address:         address,
			ProductCost:     productCost,
			ProductDiscount: 0,
			TransportCost:   transportCost,
			Total:           total,
			Note:            null.NewString(params.Note, params.Note != ""),
			Data:            json.RawMessage("{}"),
			DateCreated:     time.Now(),
		})
		if err != nil {
			return orderResult{}, sharedmodel.WrapErr("db create order", err)
		}

		// Link items to the order
		if _, err := b.storage.Querier().SetItemsOrderID(ctx, orderdb.SetItemsOrderIDParams{
			OrderID: uuid.NullUUID{UUID: order.ID, Valid: true},
			ItemIds: params.ItemIDs,
		}); err != nil {
			return orderResult{}, sharedmodel.WrapErr("db set items order id", err)
		}

		return orderResult{OrderID: order.ID.String()}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create order", err)
	}

	metrics.OrdersCreatedTotal.Inc()

	orderID, _ := uuid.Parse(oResult.OrderID)

	// Step 6: Notify buyer (fire-and-forget)
	itemNames := make([]string, 0, len(fetched.Items))
	for _, fi := range fetched.Items {
		if fi.SkuName != "" {
			itemNames = append(itemNames, fi.SkuName)
		}
	}
	summary := ordermodel.SummarizeNames(itemNames)
	notiMeta, _ := json.Marshal(map[string]string{"order_id": orderID.String()})
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: buyerID,
		Type:      accountmodel.NotiItemsConfirmed,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Items confirmed",
		Content:   fmt.Sprintf("%s has been confirmed by the seller.", summary),
		Metadata:  notiMeta,
	})

	// Step 7: Return hydrated order
	return b.GetBuyerOrder(ctx, orderID)
}

// RejectSellerPending rejects pending items owned by the seller, releases inventory, and refunds wallet.
func (b *OrderHandler) RejectSellerPending(ctx restate.Context, params RejectSellerPendingParams) error {
	// Lock: exclusive — same key as ConfirmSellerPending
	unlock := b.locker.Lock(ctx, fmt.Sprintf("order:seller-pending:%s", params.Account.ID))
	defer unlock()

	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate reject items", err)
	}

	sellerID := params.Account.ID

	// Fetch items and validate
	type itemInfo struct {
		ID                    int64  `json:"id"`
		SkuID                 string `json:"sku_id"`
		SkuName               string `json:"sku_name"`
		Quantity              int64  `json:"quantity"`
		BuyerID               string `json:"buyer_id"`
		PaidAmount            int64  `json:"paid_amount"`
		TransportCostEstimate int64  `json:"transport_cost_estimate"`
	}
	items, err := restate.Run(ctx, func(ctx restate.RunContext) ([]itemInfo, error) {
		dbItems, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			ID: params.ItemIDs,
		})
		if err != nil {
			return nil, sharedmodel.WrapErr("db list items", err)
		}
		if len(dbItems) != len(params.ItemIDs) {
			return nil, ordermodel.ErrOrderItemNotFound.Terminal()
		}

		result := make([]itemInfo, 0, len(dbItems))
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
			result = append(result, itemInfo{
				ID:                    item.ID,
				SkuID:                 item.SkuID.String(),
				SkuName:               item.SkuName,
				Quantity:              item.Quantity,
				BuyerID:               item.AccountID.String(),
				PaidAmount:            item.PaidAmount,
				TransportCostEstimate: item.TransportCostEstimate,
			})
		}
		return result, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch items", err)
	}

	// Release inventory for each item
	releaseItems := lo.Map(items, func(item itemInfo, _ int) inventorybiz.ReleaseInventoryItem {
		skuID, _ := uuid.Parse(item.SkuID)
		return inventorybiz.ReleaseInventoryItem{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   skuID,
			Amount:  item.Quantity,
		}
	})
	if err := b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
		Items: releaseItems,
	}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Cancel items
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().CancelItemsByIDs(ctx, params.ItemIDs)
		return err
	}); err != nil {
		return sharedmodel.WrapErr("db cancel items", err)
	}

	// Refund wallet for each item's paid_amount + transport_cost_estimate, grouped by buyer
	if len(items) > 0 {
		// Group items by buyer
		buyerItems := make(map[string][]itemInfo)
		for _, item := range items {
			buyerItems[item.BuyerID] = append(buyerItems[item.BuyerID], item)
		}

		for buyerIDStr, buyerItemList := range buyerItems {
			buyerID, _ := uuid.Parse(buyerIDStr)

			var totalRefund int64
			for _, item := range buyerItemList {
				totalRefund += item.PaidAmount + item.TransportCostEstimate
			}
			if totalRefund > 0 {
				if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
					AccountID: buyerID,
					Amount:    totalRefund,
					Type:      "Refund",
					Reference: fmt.Sprintf("seller-reject-items-%v", lo.Map(buyerItemList, func(it itemInfo, _ int) int64 { return it.ID })),
					Note:      "Refund for seller-rejected items",
				}); err != nil {
					return sharedmodel.WrapErr("wallet refund", err)
				}
			}

			// Notify buyer (fire-and-forget)
			rejectedNames := make([]string, 0, len(buyerItemList))
			for _, it := range buyerItemList {
				if it.SkuName != "" {
					rejectedNames = append(rejectedNames, it.SkuName)
				}
			}
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
	}

	return nil
}
