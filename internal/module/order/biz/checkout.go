package orderbiz

import (
	"fmt"
	"strings"

	restate "github.com/restatedev/sdk-go"

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
	"shopnexus-server/internal/infras/metrics"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// BuyerCheckout creates pending order items (no order, no payment, no transport yet).
// Inventory is reserved. Items are removed from cart unless BuyNow.
func (b *OrderHandler) BuyerCheckout(ctx restate.Context, params BuyerCheckoutParams) (_ BuyerCheckoutResult, err error) {
	defer metrics.TrackHandler("order", "BuyerCheckout", &err)()

	var zero BuyerCheckoutResult

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate checkout", err)
	}
	if params.BuyNow && len(params.Items) != 1 {
		return zero, ordermodel.ErrBuyNowSingleSkuOnly.Terminal()
	}

	skuIDs := lo.Map(params.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	checkoutItemMap := lo.KeyBy(params.Items, func(s CheckoutItem) uuid.UUID { return s.SkuID })

	// Step 1: Fetch product data (SKUs + SPUs for seller_id and name)
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

	// Step 2: Reserve inventory
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

	serialIDsMap := lo.SliceToMap(inventories, func(i inventorybiz.ReserveInventoryResult) (uuid.UUID, []string) {
		return i.RefID, i.SerialIDs
	})

	// Step 3: Create pending items
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

			jsonSerialIDs, err := sonic.Marshal(serialIDs)
			if err != nil {
				return nil, sharedmodel.WrapErr("marshal serial ids", err)
			}

			paidAmount := int64(sku.Price) * checkoutItem.Quantity

			// Build display name: "SPU Name - Attr1 / Attr2"
			skuName := spu.Name
			if len(sku.Attributes) > 0 {
				vals := make([]string, 0, len(sku.Attributes))
				for _, attr := range sku.Attributes {
					vals = append(vals, attr.Value)
				}
				skuName += " - " + strings.Join(vals, " / ")
			}

			dbItem, err := b.storage.Querier().CreatePendingItem(ctx, orderdb.CreatePendingItemParams{
				AccountID:  params.Account.ID,
				SellerID:   spu.AccountID,
				Address:    checkoutItem.Address,
				SkuID:      sku.ID,
				SkuName:    skuName,
				Quantity:   checkoutItem.Quantity,
				UnitPrice:  int64(sku.Price),
				PaidAmount: paidAmount,
				Note:       null.NewString(checkoutItem.Note, checkoutItem.Note != ""),
				SerialIds:  jsonSerialIDs,
			})
			if err != nil {
				return nil, sharedmodel.WrapErr("db create pending item", err)
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
		return zero, sharedmodel.WrapErr("create pending items", err)
	}

	metrics.CheckoutItemsCreatedTotal.WithLabelValues("success").Add(float64(len(createdItems)))

	// Step 4: Remove from cart (skip if BuyNow)
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

	// Step 5: Track purchase interactions (fire-and-forget)
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

	// Step 5b: Notify sellers about new pending items (fire-and-forget)
	// Group product names by seller
	sellerItems := make(map[uuid.UUID][]string)
	for _, item := range params.Items {
		sku := skuMap[item.SkuID]
		spu := spuMap[sku.SpuID]
		sellerItems[spu.AccountID] = append(sellerItems[spu.AccountID], spu.Name)
	}
	for sellerID, names := range sellerItems {
		summary := names[0]
		if len(names) > 1 {
			summary = fmt.Sprintf("%s and %d more", names[0], len(names)-1)
		}
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: sellerID,
			Type:      accountmodel.NotiNewPendingItems,
			Channel:   accountmodel.ChannelInApp,
			Title:     "New pending items",
			Content:   fmt.Sprintf("New order for %s is waiting for your review.", summary),
		})
	}

	// Step 6: Hydrate and return created items
	itemIDs := lo.Map(createdItems, func(info createdItemInfo, _ int) int64 { return info.ID })

	hydratedItems, err := b.hydrateItems(ctx, itemIDs)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate created items", err)
	}

	return BuyerCheckoutResult{
		Items: hydratedItems,
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

		result = append(result, ordermodel.OrderItem{
			ID:          oi.ID,
			OrderID:     orderID,
			AccountID:   oi.AccountID,
			SellerID:    oi.SellerID,
			Address:     oi.Address,
			Status:      oi.Status,
			SkuID:       oi.SkuID,
			SpuID:       spuID,
			SkuName:     oi.SkuName,
			Quantity:    oi.Quantity,
			UnitPrice:   sharedmodel.Concurrency(oi.UnitPrice),
			PaidAmount:  oi.PaidAmount,
			Note:        note,
			SerialIds:   oi.SerialIds,
			DateCreated: oi.DateCreated,
			Resources:   resourcesMap[spuID],
		})
	}

	return result, nil
}

// ListBuyerPending returns paginated pending items for the buyer.
func (b *OrderHandler) ListBuyerPending(ctx restate.Context, params ListBuyerPendingParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list pending items", err)
	}

	status := params.Status
	if len(status) == 0 {
		status = []orderdb.OrderItemStatus{orderdb.OrderItemStatusPending}
	}

	type pendingResult struct {
		Items []orderdb.OrderItem `json:"items"`
		Total int64               `json:"total"`
	}

	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (pendingResult, error) {
		items, err := b.storage.Querier().ListPendingItemsByAccount(ctx, orderdb.ListPendingItemsByAccountParams{
			AccountID: params.AccountID,
			Status:    status,
			Offset:    params.Offset(),
			Limit:     params.Limit,
		})
		if err != nil {
			return pendingResult{}, err
		}

		total, err := b.storage.Querier().CountPendingItemsByAccount(ctx, orderdb.CountPendingItemsByAccountParams{
			AccountID: params.AccountID,
			Status:    status,
		})
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

// CancelBuyerPending cancels a pending item and releases its inventory.
func (b *OrderHandler) CancelBuyerPending(ctx restate.Context, params CancelBuyerPendingParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate cancel pending item", err)
	}

	// Fetch the item first
	type itemInfo struct {
		SkuID    string `json:"sku_id"`
		SellerID string `json:"seller_id"`
		Quantity int64  `json:"quantity"`
		Status   string `json:"status"`
	}
	info, err := restate.Run(ctx, func(ctx restate.RunContext) (itemInfo, error) {
		item, err := b.storage.Querier().GetItem(ctx, null.IntFrom(params.ItemID))
		if err != nil {
			return itemInfo{}, sharedmodel.WrapErr("db get item", err)
		}
		if item.AccountID != params.AccountID {
			return itemInfo{}, ordermodel.ErrOrderItemNotFound.Terminal()
		}
		if item.Status != orderdb.OrderItemStatusPending {
			return itemInfo{}, ordermodel.ErrItemNotPending
		}
		return itemInfo{
			SkuID:    item.SkuID.String(),
			SellerID: item.SellerID.String(),
			Quantity: item.Quantity,
			Status:   string(item.Status),
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
		return b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
			ID:        params.ItemID,
			AccountID: params.AccountID,
		})
	}); err != nil {
		return err
	}

	// Notify seller: pending item cancelled (fire-and-forget)
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
