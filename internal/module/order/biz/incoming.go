package orderbiz

import (
	"encoding/json"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
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

// ListIncomingItems returns paginated pending items for the seller.
func (b *OrderHandler) ListIncomingItems(ctx restate.Context, params ListIncomingItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	type incomingResult struct {
		Items []orderdb.OrderItem `json:"items"`
		Total int64               `json:"total"`
	}

	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (incomingResult, error) {
		items, err := b.storage.Querier().ListPendingItemsBySeller(ctx, orderdb.ListPendingItemsBySellerParams{
			SellerID: params.SellerID,
			Search:   params.Search,
			Offset:   params.Offset(),
			Limit:    params.Limit,
		})
		if err != nil {
			return incomingResult{}, err
		}

		total, err := b.storage.Querier().CountPendingItemsBySeller(ctx, orderdb.CountPendingItemsBySellerParams{
			SellerID: params.SellerID,
			Search:   params.Search,
		})
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

// ConfirmItems groups selected pending items into an order with transport.
func (b *OrderHandler) ConfirmItems(ctx restate.Context, params ConfirmItemsParams) (ordermodel.Order, error) {
	var zero ordermodel.Order

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate confirm items", err)
	}

	// Step 1: Fetch items and validate
	type fetchedItem struct {
		ID         int64  `json:"id"`
		AccountID  string `json:"account_id"`
		SellerID   string `json:"seller_id"`
		Address    string `json:"address"`
		SkuID      string `json:"sku_id"`
		Quantity   int64  `json:"quantity"`
		UnitPrice  int64  `json:"unit_price"`
		PaidAmount int64  `json:"paid_amount"`
		Status     string `json:"status"`
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
				ID:         item.ID,
				AccountID:  item.AccountID.String(),
				SellerID:   item.SellerID.String(),
				Address:    item.Address,
				SkuID:      item.SkuID.String(),
				Quantity:   item.Quantity,
				UnitPrice:  item.UnitPrice,
				PaidAmount: item.PaidAmount,
				Status:     string(item.Status),
			})
		}
		return fetchResult{Items: result}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("fetch items", err)
	}

	// Validate all items: Pending + same seller + same buyer + same address
	sellerID := params.Account.ID
	var buyerID uuid.UUID
	var address string
	for i, item := range fetched.Items {
		if item.Status != string(orderdb.OrderItemStatusPending) {
			return zero, ordermodel.ErrItemNotPending
		}
		itemSellerID, _ := uuid.Parse(item.SellerID)
		if itemSellerID != sellerID {
			return zero, ordermodel.ErrItemNotOwnedBySeller
		}
		itemBuyerID, _ := uuid.Parse(item.AccountID)
		if i == 0 {
			buyerID = itemBuyerID
			address = item.Address
		} else {
			if itemBuyerID != buyerID {
				return zero, ordermodel.ErrItemsNotSameBuyer
			}
			if item.Address != address {
				return zero, ordermodel.ErrItemsNotSameAddress
			}
		}
	}

	// Step 2: Get seller's default contact address (for transport from_address)
	contactMap, err := b.account.GetDefaultContact(ctx, []uuid.UUID{sellerID})
	if err != nil {
		return zero, sharedmodel.WrapErr("get seller contact", err)
	}
	fromAddress := contactMap[sellerID].Address

	// Step 3: Get transport client, quote and create transport
	transportClient, err := b.getTransportClient(params.TransportOption)
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
			Option:      params.TransportOption,
		})
		if err != nil {
			return transportResult{}, sharedmodel.WrapErr("create transport", err)
		}

		// Store transport in DB
		dbTransport, err := b.storage.Querier().CreateTransport(ctx, orderdb.CreateTransportParams{
			ID:     created.ID,
			Option: params.TransportOption,
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

	// Step 4: Calculate costs
	var productCost int64
	var totalPaidAmount int64
	for _, item := range fetched.Items {
		productCost += item.UnitPrice * item.Quantity
		totalPaidAmount += item.PaidAmount
	}
	productDiscount := productCost - totalPaidAmount
	if productDiscount < 0 {
		productDiscount = 0
	}
	transportCost := tResult.Cost
	total := productCost - productDiscount + transportCost

	// Step 5: Create order and confirm items
	type orderResult struct {
		OrderID string `json:"order_id"`
	}
	oResult, err := restate.Run(ctx, func(ctx restate.RunContext) (orderResult, error) {
		order, err := b.storage.Querier().CreateOrder(ctx, orderdb.CreateOrderParams{
			ID:              uuid.Must(uuid.NewRandom()),
			BuyerID:         buyerID,
			SellerID:        sellerID,
			TransportID:     uuid.NullUUID{UUID: transportID, Valid: true},
			Status:          orderdb.OrderStatusPending,
			Address:         address,
			ProductCost:     productCost,
			ProductDiscount: productDiscount,
			TransportCost:   transportCost,
			Total:           total,
			Note:            null.NewString(params.Note, params.Note != ""),
			Data:            json.RawMessage("{}"),
		})
		if err != nil {
			return orderResult{}, sharedmodel.WrapErr("db create order", err)
		}

		// Confirm items (set order_id, status=Confirmed)
		if err := b.storage.Querier().ConfirmItems(ctx, orderdb.ConfirmItemsParams{
			OrderID: uuid.NullUUID{UUID: order.ID, Valid: true},
			Ids:     params.ItemIDs,
		}); err != nil {
			return orderResult{}, sharedmodel.WrapErr("db confirm items", err)
		}

		return orderResult{OrderID: order.ID.String()}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("create order", err)
	}

	orderID, _ := uuid.Parse(oResult.OrderID)

	// Step 6: Notify buyer (fire-and-forget)
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: buyerID,
		Type:      "items_confirmed",
		Channel:   "in_app",
		Title:     "Items confirmed",
		Content:   fmt.Sprintf("Your items have been confirmed and grouped into order %s.", orderID),
		Metadata:  json.RawMessage(fmt.Sprintf(`{"order_id":"%s"}`, orderID)),
	})

	// Step 7: Return hydrated order
	return b.GetOrder(ctx, orderID)
}

// RejectItems rejects pending items owned by the seller and releases inventory.
func (b *OrderHandler) RejectItems(ctx restate.Context, params RejectItemsParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate reject items", err)
	}

	sellerID := params.Account.ID

	// Fetch items and validate
	type itemInfo struct {
		ID       int64  `json:"id"`
		SkuID    string `json:"sku_id"`
		Quantity int64  `json:"quantity"`
		BuyerID  string `json:"buyer_id"`
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
			if item.Status != orderdb.OrderItemStatusPending {
				return nil, ordermodel.ErrItemNotPending
			}
			if item.SellerID != sellerID {
				return nil, ordermodel.ErrItemNotOwnedBySeller
			}
			result = append(result, itemInfo{
				ID:       item.ID,
				SkuID:    item.SkuID.String(),
				Quantity: item.Quantity,
				BuyerID:  item.AccountID.String(),
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
		return b.storage.Querier().CancelItemsBySeller(ctx, orderdb.CancelItemsBySellerParams{
			Ids:      params.ItemIDs,
			SellerID: sellerID,
		})
	}); err != nil {
		return sharedmodel.WrapErr("db cancel items", err)
	}

	// Notify buyer (fire-and-forget)
	if len(items) > 0 {
		buyerID, _ := uuid.Parse(items[0].BuyerID)
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: buyerID,
			Type:      "items_rejected",
			Channel:   "in_app",
			Title:     "Items rejected",
			Content:   "Some of your items have been rejected by the seller.",
			Metadata:  json.RawMessage(`{}`),
		})
	}

	return nil
}
