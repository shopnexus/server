package orderbiz

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// CancelUnpaidCheckout is called 15 minutes after checkout if payment has not been confirmed.
// It cancels all pending items linked to the payment, releases inventory, refunds wallet, and notifies the buyer.
func (b *OrderHandler) CancelUnpaidCheckout(ctx restate.Context, paymentID int64) (err error) {
	defer metrics.TrackHandler("order", "CancelUnpaidCheckout", &err)()

	// Distributed lock per payment — prevents race with ConfirmPayment
	unlock := b.locker.Lock(ctx, fmt.Sprintf("order:payment:%d", paymentID))
	defer unlock()

	// Fetch pending items for this payment
	type fetchResult struct {
		Items   []orderdb.OrderItem `json:"items"`
		BuyerID uuid.UUID           `json:"account_id"`
	}

	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (fetchResult, error) {
		items, err := b.storage.Querier().ListPendingPaymentItemsByPaymentID(ctx, null.IntFrom(paymentID))
		if err != nil {
			return fetchResult{}, sharedmodel.WrapErr("db list pending items by payment", err)
		}
		if len(items) == 0 {
			return fetchResult{}, nil
		}

		return fetchResult{
			Items:   items,
			BuyerID: items[0].AccountID,
		}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch pending items", err)
	}

	// No items found — already handled
	if len(fetched.Items) == 0 {
		return nil
	}

	itemIDs := lo.Map(fetched.Items, func(i orderdb.OrderItem, _ int) int64 { return i.ID })

	// Cancel the items
	if err = restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err = b.storage.Querier().CancelItemsByIDs(ctx, itemIDs)
		return err
	}); err != nil {
		return sharedmodel.WrapErr("cancel items", err)
	}

	// Cancel the payment (update status to Cancelled)
	if err = restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
			ID: paymentID,
			Status: orderdb.NullOrderStatus{
				OrderStatus: orderdb.OrderStatusCancelled,
				Valid:       true,
			},
		})
		return err
	}); err != nil {
		return sharedmodel.WrapErr("cancel payment", err)
	}

	// Release inventory per item
	releaseItems := make([]inventorybiz.ReleaseInventoryItem, 0, len(fetched.Items))
	for _, item := range fetched.Items {
		releaseItems = append(releaseItems, inventorybiz.ReleaseInventoryItem{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   item.SkuID,
			Amount:  item.Quantity,
		})
	}
	if err = b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
		Items: releaseItems,
	}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Refund wallet (credit back paid_amount + transport_cost_estimate per item)
	var totalRefund int64
	for _, item := range fetched.Items {
		totalRefund += item.PaidAmount + item.TransportCostEstimate
	}
	if err = b.refundBuyerWallet(ctx, fetched.BuyerID, totalRefund, paymentID); err != nil {
		return sharedmodel.WrapErr("refund buyer wallet", err)
	}

	metrics.PaymentsTotal.WithLabelValues("Cancelled", "timeout").Inc()

	// Notify buyer
	itemNames := lo.Map(fetched.Items, func(i orderdb.OrderItem, _ int) string { return i.SkuName })
	summary := ordermodel.SummarizeNames(itemNames)
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: fetched.BuyerID,
		Type:      accountmodel.NotiOrderCancelled,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Payment expired",
		Content:   fmt.Sprintf("Your checkout for %s was cancelled because payment was not received in time.", summary),
	})

	return nil
}

// AutoCancelPendingItems is called 48 hours after payment is confirmed if the seller hasn't confirmed.
// It cancels remaining pending items, releases inventory, refunds wallet, and notifies both buyer and sellers.
func (b *OrderHandler) AutoCancelPendingItems(ctx restate.Context, paymentID int64) error {
	var err error
	defer metrics.TrackHandler("order", "AutoCancelPendingItems", &err)()

	// Fetch pending items for this payment (still without order_id and not cancelled)
	type fetchResult struct {
		Items   []orderdb.OrderItem `json:"items"`
		BuyerID uuid.UUID           `json:"buyer_id"`
	}

	fetched, err := restate.Run(ctx, func(ctx restate.RunContext) (fetchResult, error) {
		items, err := b.storage.Querier().ListPendingPaymentItemsByPaymentID(ctx, null.IntFrom(paymentID))
		if err != nil {
			return fetchResult{}, sharedmodel.WrapErr("db list pending items by payment", err)
		}
		if len(items) == 0 {
			return fetchResult{}, nil
		}

		return fetchResult{
			Items:   items,
			BuyerID: items[0].AccountID,
		}, nil
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch pending items", err)
	}

	// No items found — all already confirmed or cancelled
	if len(fetched.Items) == 0 {
		return nil
	}

	// Cancel the items
	if err = restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err = b.storage.Querier().CancelItemsByIDs(
			ctx,
			lo.Map(fetched.Items, func(i orderdb.OrderItem, _ int) int64 { return i.ID }),
		)
		return err
	}); err != nil {
		return sharedmodel.WrapErr("cancel items", err)
	}

	// Release inventory per item
	releaseItems := make([]inventorybiz.ReleaseInventoryItem, 0, len(fetched.Items))
	for _, item := range fetched.Items {
		releaseItems = append(releaseItems, inventorybiz.ReleaseInventoryItem{
			RefType: inventorydb.InventoryStockRefTypeProductSku,
			RefID:   item.SkuID,
			Amount:  item.Quantity,
		})
	}
	if err = b.inventory.ReleaseInventory(ctx, inventorybiz.ReleaseInventoryParams{
		Items: releaseItems,
	}); err != nil {
		return sharedmodel.WrapErr("release inventory", err)
	}

	// Refund wallet
	var totalRefund int64
	for _, item := range fetched.Items {
		totalRefund += item.PaidAmount + item.TransportCostEstimate
	}
	if err = b.refundBuyerWallet(ctx, fetched.BuyerID, totalRefund, paymentID); err != nil {
		return sharedmodel.WrapErr("refund buyer wallet", err)
	}

	// Notify buyer
	itemNames := lo.Map(fetched.Items, func(i orderdb.OrderItem, _ int) string { return i.SkuName })
	summary := ordermodel.SummarizeNames(itemNames)
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: fetched.BuyerID,
		Type:      accountmodel.NotiOrderCancelled,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Order auto-cancelled",
		Content:   fmt.Sprintf("Your order for %s was cancelled because the seller did not confirm in time. A refund has been issued.", summary),
	})

	// Notify sellers — group items by seller
	sellerItemNames := make(map[uuid.UUID][]string)
	for _, item := range fetched.Items {
		sellerItemNames[item.SellerID] = append(sellerItemNames[item.SellerID], item.SkuName)
	}
	for sellerID, names := range sellerItemNames {
		sellerSummary := ordermodel.SummarizeNames(names)
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: sellerID,
			Type:      accountmodel.NotiPendingItemCancelled,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Pending items auto-cancelled",
			Content:   fmt.Sprintf("Items for %s were cancelled because confirmation was not received within 48 hours.", sellerSummary),
		})
	}

	return nil
}
