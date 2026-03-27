package orderbiz

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

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
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/lo"
)

// Checkout creates pending order items (no order, no payment, no transport yet).
// Inventory is reserved. Items are removed from cart unless BuyNow.
func (b *OrderHandler) Checkout(ctx restate.Context, params CheckoutParams) (CheckoutResult, error) {
	var zero CheckoutResult

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
		return zero, sharedmodel.WrapErr("reserve inventory", err)
	}

	serialIDsMap := lo.SliceToMap(inventories, func(i inventorybiz.ReserveInventoryResult) (uuid.UUID, []string) {
		return i.RefID, i.SerialIDs
	})

	// Step 3: Create pending items
	type createdItemInfo struct {
		ID          int64     `json:"id"`
		SkuID       string    `json:"sku_id"`
		DateCreated string    `json:"date_created"`
	}
	createdItems, err := restate.Run(ctx, func(ctx restate.RunContext) ([]createdItemInfo, error) {
		var items []createdItemInfo
		for _, checkoutItem := range params.Items {
			sku := skuMap[checkoutItem.SkuID]
			spu := spuMap[sku.SpuID]
			serialIDs := serialIDsMap[checkoutItem.SkuID]

			jsonSerialIDs, err := sonic.Marshal(serialIDs)
			if err != nil {
				return nil, fmt.Errorf("marshal serial ids: %w", err)
			}

			paidAmount := int64(sku.Price) * checkoutItem.Quantity

			dbItem, err := b.storage.Querier().CreatePendingItem(ctx, orderdb.CreatePendingItemParams{
				AccountID:  params.Account.ID,
				SellerID:   spu.AccountID,
				Address:    checkoutItem.Address,
				SkuID:      sku.ID,
				SkuName:    spu.Name,
				Quantity:   checkoutItem.Quantity,
				UnitPrice:  int64(sku.Price),
				PaidAmount: paidAmount,
				Note:       null.NewString(checkoutItem.Note, checkoutItem.Note != ""),
				SerialIds:  jsonSerialIDs,
			})
			if err != nil {
				return nil, fmt.Errorf("create pending item: %w", err)
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
		return zero, sharedmodel.WrapErr("create pending items", err)
	}

	// Step 4: Remove from cart (skip if BuyNow)
	if !params.BuyNow {
		if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
			cartItems, err := b.storage.Querier().RemoveCheckoutItem(ctx, orderdb.RemoveCheckoutItemParams{
				AccountID: params.Account.ID,
				SkuID:     skuIDs,
			})
			if err != nil {
				return fmt.Errorf("remove checkout items: %w", err)
			}
			if len(cartItems) != len(skuIDs) {
				// Some items may not be in cart (e.g., added via BuyNow previously), that's OK
				_ = cartItems
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

	// Step 6: Hydrate and return created items
	itemIDs := lo.Map(createdItems, func(info createdItemInfo, _ int) int64 { return info.ID })

	hydratedItems, err := b.hydrateItems(ctx, itemIDs)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate created items", err)
	}

	return CheckoutResult{
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
			SkuName:     oi.SkuName,
			Quantity:    oi.Quantity,
			UnitPrice:   oi.UnitPrice,
			PaidAmount:  oi.PaidAmount,
			Note:        note,
			SerialIds:   oi.SerialIds,
			DateCreated: oi.DateCreated,
			Resources:   resourcesMap[spuID],
		})
	}

	return result, nil
}

// ListPendingItems returns paginated pending items for the buyer.
func (b *OrderHandler) ListPendingItems(ctx restate.Context, params ListPendingItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	if err := validator.Validate(params); err != nil {
		return zero, restate.TerminalErrorf("validate list pending items: %w", err)
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
		return zero, fmt.Errorf("list pending items: %w", err)
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

// CancelPendingItem cancels a pending item and releases its inventory.
func (b *OrderHandler) CancelPendingItem(ctx restate.Context, params CancelPendingItemParams) error {
	if err := validator.Validate(params); err != nil {
		return restate.TerminalErrorf("validate cancel pending item: %w", err)
	}

	// Fetch the item first
	type itemInfo struct {
		SkuID    string `json:"sku_id"`
		Quantity int64  `json:"quantity"`
		Status   string `json:"status"`
	}
	info, err := restate.Run(ctx, func(ctx restate.RunContext) (itemInfo, error) {
		item, err := b.storage.Querier().GetItem(ctx, orderdb.GetItemParams{
			ID: pgtype.Int8{Int64: params.ItemID, Valid: true},
		})
		if err != nil {
			return itemInfo{}, fmt.Errorf("get item: %w", err)
		}
		if item.AccountID != params.AccountID {
			return itemInfo{}, ordermodel.ErrOrderItemNotFound.Terminal()
		}
		if item.Status != orderdb.OrderItemStatusPending {
			return itemInfo{}, ordermodel.ErrItemNotPending
		}
		return itemInfo{
			SkuID:    item.SkuID.String(),
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
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		return b.storage.Querier().CancelItem(ctx, orderdb.CancelItemParams{
			ID:        params.ItemID,
			AccountID: params.AccountID,
		})
	})
}

