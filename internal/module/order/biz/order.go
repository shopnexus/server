package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// GetBuyerOrder returns a single order by ID with all items and payment details.
func (b *OrderHandler) GetBuyerOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	var zero ordermodel.Order

	orders, err := b.ListBuyerConfirmed(ctx, ListBuyerConfirmedParams{
		ID: []uuid.UUID{orderID},
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get order", err)
	}
	if len(orders.Data) == 0 {
		return zero, ordermodel.ErrOrderNotFound.Terminal()
	}

	return orders.Data[0], nil
}

// GetSellerOrder returns a single order by ID (seller perspective).
func (b *OrderHandler) GetSellerOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	return b.GetBuyerOrder(ctx, orderID)
}

// ListBuyerConfirmed returns paginated orders with hydrated items, payments, and product resources.
func (b *OrderHandler) ListBuyerConfirmed(ctx restate.Context, params ListBuyerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list orders", err)
	}

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountOrderRow, error) {
		return b.storage.Querier().ListCountOrder(ctx, orderdb.ListCountOrderParams{
			Limit:  params.Limit,
			Offset: params.Offset(),
			ID:     params.ID,
		})
	})

	if err != nil {
		return zero, sharedmodel.WrapErr("db list orders", err)
	}

	var total null.Int64
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders := lo.Map(listCountOrder, func(item orderdb.ListCountOrderRow, _ int) orderdb.OrderOrder {
		return item.OrderOrder
	})
	data, err := b.hydrateOrders(ctx, orders)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate orders", err)
	}

	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       data,
	}, nil
}

// ListSellerConfirmed returns paginated orders for the seller with optional payment/order status filters.
func (b *OrderHandler) ListSellerConfirmed(ctx restate.Context, params ListSellerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list seller orders", err)
	}

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountSellerOrderRow, error) {
		return b.storage.Querier().ListCountSellerOrder(ctx, orderdb.ListCountSellerOrderParams{
			SellerID:      params.SellerID,
			Search:        params.Search,
			PaymentStatus: params.PaymentStatus,
			OrderStatus:   params.OrderStatus,
			Offset:        params.Offset(),
			Limit:         params.Limit,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list seller orders", err)
	}

	var total null.Int64
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders, err := b.hydrateOrders(ctx, lo.Map(listCountOrder, func(item orderdb.ListCountSellerOrderRow, _ int) orderdb.OrderOrder {
		return item.OrderOrder
	}))
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate seller orders", err)
	}

	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       orders,
	}, nil
}

func (b *OrderHandler) hydrateOrders(ctx restate.Context, orders []orderdb.OrderOrder) ([]ordermodel.Order, error) {
	if len(orders) == 0 {
		return []ordermodel.Order{}, nil
	}

	orderIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) uuid.UUID { return o.ID })

	// Collect payment IDs (only valid ones)
	var paymentIDs []int64
	for _, o := range orders {
		if o.PaymentID.Valid {
			paymentIDs = append(paymentIDs, o.PaymentID.Int64)
		}
	}

	// Fetch order items and payments from DB inside Run
	type dbResults struct {
		OrderItems []orderdb.OrderItem    `json:"order_items"`
		Payments   []orderdb.OrderPayment `json:"payments"`
	}
	dbData, err := restate.Run(ctx, func(ctx restate.RunContext) (dbResults, error) {
		orderItems, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			OrderID: lo.Map(orderIDs, func(id uuid.UUID, _ int) uuid.NullUUID {
				return uuid.NullUUID{UUID: id, Valid: true}
			}),
		})
		if err != nil {
			return dbResults{}, err
		}

		var payments []orderdb.OrderPayment
		if len(paymentIDs) > 0 {
			payments, err = b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
				ID: paymentIDs,
			})
			if err != nil {
				return dbResults{}, err
			}
		}

		return dbResults{OrderItems: orderItems, Payments: payments}, nil
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db fetch order data", err)
	}

	// Group items by order_id
	orderItemsMap := make(map[uuid.UUID][]orderdb.OrderItem)
	for _, oi := range dbData.OrderItems {
		if oi.OrderID.Valid {
			orderItemsMap[oi.OrderID.UUID] = append(orderItemsMap[oi.OrderID.UUID], oi)
		}
	}

	// Enrich items with resources
	enrichedItemsMap := make(map[uuid.UUID][]ordermodel.OrderItem)
	for orderID, items := range orderItemsMap {
		enriched, err := b.enrichItems(ctx, items)
		if err != nil {
			return nil, sharedmodel.WrapErr("enrich order items", err)
		}
		enrichedItemsMap[orderID] = enriched
	}

	paymentMap := lo.KeyBy(dbData.Payments, func(p orderdb.OrderPayment) int64 { return p.ID })

	result := make([]ordermodel.Order, 0, len(orders))
	for _, o := range orders {
		var paymentPtr *ordermodel.Payment
		if o.PaymentID.Valid {
			if p, ok := paymentMap[o.PaymentID.Int64]; ok {
				var datePaid *time.Time
				if p.DatePaid.Valid {
					datePaid = &p.DatePaid.Time
				}
				var pmID *uuid.UUID
				if p.PaymentMethodID.Valid {
					pmID = &p.PaymentMethodID.UUID
				}
				paymentPtr = &ordermodel.Payment{
					ID:              p.ID,
					AccountID:       p.AccountID,
					Option:          p.Option,
					PaymentMethodID: pmID,
					Status:          p.Status,
					Amount:          sharedmodel.Concurrency(p.Amount),
					Data:            p.Data,
					DateCreated:     p.DateCreated,
					DatePaid:        datePaid,
					DateExpired:     p.DateExpired,
				}
			}
		}


		result = append(result, ordermodel.Order{
			ID:              o.ID,
			BuyerID:         o.BuyerID,
			SellerID:        o.SellerID,
			TransportID:     o.TransportID,
			Payment:         paymentPtr,
			Status:          o.Status,
			Address:         o.Address,
			ProductCost:     sharedmodel.Concurrency(o.ProductCost),
			ProductDiscount: sharedmodel.Concurrency(o.ProductDiscount),
			TransportCost:   sharedmodel.Concurrency(o.TransportCost),
			Total:           sharedmodel.Concurrency(o.Total),
			Note:            o.Note,
			Data:            o.Data,
			DateCreated:     o.DateCreated,
			Items:           enrichedItemsMap[o.ID],
		})
	}

	return result, nil
}

// ConfirmPayment updates the payment status based on a webhook callback result.
// Called by the transport layer after provider-specific webhook verification.
func (b *OrderHandler) ConfirmPayment(ctx restate.Context, params ConfirmPaymentParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate confirm payment", err)
	}

	refUUID, err := uuid.Parse(params.RefID)
	if err != nil {
		return sharedmodel.WrapErr("parse ref id", err)
	}

	var dbStatus orderdb.OrderStatus
	switch params.Status {
	case payment.StatusSuccess:
		dbStatus = orderdb.OrderStatusSuccess
	case payment.StatusFailed:
		dbStatus = orderdb.OrderStatusFailed
	default:
		return nil // ignore pending/expired — no state change needed
	}

	type confirmResult struct {
		BuyerID     string `json:"buyer_id"`
		RedirectURL string `json:"redirect_url"`
	}
	cr, err := restate.Run(ctx, func(ctx restate.RunContext) (confirmResult, error) {
		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: refUUID, Valid: true})
		if err != nil {
			return confirmResult{}, err
		}

		if !order.PaymentID.Valid {
			return confirmResult{}, ordermodel.ErrMissingPayment.Terminal()
		}

		_, err = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
			ID:     order.PaymentID.Int64,
			Status: orderdb.NullOrderStatus{OrderStatus: dbStatus, Valid: true},
		})
		if err != nil {
			return confirmResult{}, err
		}

		// Extract redirect_url from payment data for notification metadata
		var redirectURL string
		payments, pErr := b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
			ID: []int64{order.PaymentID.Int64},
		})
		if pErr == nil && len(payments) > 0 {
			var data struct {
				RedirectURL string `json:"redirect_url"`
			}
			_ = json.Unmarshal(payments[0].Data, &data)
			redirectURL = data.RedirectURL
		}

		return confirmResult{BuyerID: order.BuyerID.String(), RedirectURL: redirectURL}, nil
	})
	if err != nil {
		return err
	}

	// Notify buyer about payment result
	buyerID, _ := uuid.Parse(cr.BuyerID)
	switch params.Status {
	case payment.StatusSuccess:
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: buyerID,
			Type:      accountmodel.NotiPaymentSuccess,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Payment successful",
			Content:   "Your payment has been confirmed successfully.",
			Metadata:  json.RawMessage(fmt.Sprintf(`{"order_id":"%s"}`, refUUID)),
		})
	case payment.StatusFailed:
		meta := fmt.Sprintf(`{"order_id":"%s"`, refUUID)
		if cr.RedirectURL != "" {
			meta += fmt.Sprintf(`,"redirect_url":"%s"`, cr.RedirectURL)
		}
		meta += "}"
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: buyerID,
			Type:      accountmodel.NotiPaymentFailed,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Payment failed",
			Content:   "Your payment could not be processed. Please try again.",
			Metadata:  json.RawMessage(meta),
		})
	}

	return nil
}

// CancelBuyerOrder cancels a pending order along with its items.
// If unpaid (payment_id NULL): cancel order + items, release inventory.
// If paid: return error (should use refund flow).
func (b *OrderHandler) CancelBuyerOrder(ctx restate.Context, params CancelBuyerOrderParams) error {
	// GetBuyerOrder has its own Run internally
	order, err := b.GetBuyerOrder(ctx, params.OrderID)
	if err != nil {
		return sharedmodel.WrapErr("fetch order", err)
	}

	if order.BuyerID != params.Account.ID {
		return ordermodel.ErrOrderNotFound.Terminal()
	}

	if order.Status != orderdb.OrderStatusPending {
		return ordermodel.ErrOrderCannotCancel.Terminal()
	}

	// If paid, cannot cancel directly
	if order.Payment != nil && order.Payment.Status == orderdb.OrderStatusSuccess {
		return ordermodel.ErrPaymentCannotCancel.Terminal()
	}

	// Cancel order + items
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		// Cancel payment if exists
		if order.Payment != nil {
			if _, err := b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
				ID:     order.Payment.ID,
				Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
			}); err != nil {
				return sharedmodel.WrapErr("db update payment status", err)
			}
		}

		// Cancel order
		if _, err := b.storage.Querier().UpdateOrder(ctx, orderdb.UpdateOrderParams{
			ID:     order.ID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return sharedmodel.WrapErr("db update order status", err)
		}

		// Cancel items
		if err := b.storage.Querier().CancelItemsByOrder(ctx, uuid.NullUUID{UUID: order.ID, Valid: true}); err != nil {
			return sharedmodel.WrapErr("db cancel items", err)
		}

		return nil
	}); err != nil {
		return sharedmodel.WrapErr("cancel order", err)
	}

	// Release inventory for all items
	if len(order.Items) > 0 {
		releaseItems := lo.Map(order.Items, func(item ordermodel.OrderItem, _ int) inventorybiz.ReleaseInventoryItem {
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
	}

	// Notify buyer: order cancelled
	restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
		AccountID: order.BuyerID,
		Type:      accountmodel.NotiOrderCancelled,
		Channel:   accountmodel.ChannelInApp,
		Title:     "Order cancelled",
		Content:   fmt.Sprintf("Your order for %s has been cancelled.", ordermodel.SummarizeItems(order.Items)),
		Metadata:  json.RawMessage(fmt.Sprintf(`{"order_id":"%s"}`, order.ID)),
	})

	// Track cancel_order interactions
	var cancelInteractions []analyticbiz.CreateInteraction
	for _, item := range order.Items {
		cancelInteractions = append(cancelInteractions, analyticbiz.CreateInteraction{
			Account:   params.Account,
			EventType: analyticmodel.EventCancelOrder,
			RefType:   analyticdb.AnalyticInteractionRefTypeProduct,
			RefID:     item.SkuID.String(),
		})
	}
	restate.ServiceSend(ctx, "Analytic", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
		Interactions: cancelInteractions,
	})

	return nil
}

// HasPurchasedProduct checks if an account has a successful order containing any of the given SKU IDs.
func (b *OrderHandler) HasPurchasedProduct(ctx restate.Context, params HasPurchasedProductParams) (bool, error) {
	if err := validator.Validate(params); err != nil {
		return false, sharedmodel.WrapErr("validate has purchased product", err)
	}

	return b.storage.Querier().HasPurchasedSku(ctx, orderdb.HasPurchasedSkuParams{
		AccountID: params.AccountID,
		SkuIds:    params.SkuIDs,
	})
}

// ListReviewableOrders returns successful orders that contain items matching the given SKU IDs.
func (b *OrderHandler) ListReviewableOrders(ctx restate.Context, params ListReviewableOrdersParams) ([]ReviewableOrder, error) {
	if err := validator.Validate(params); err != nil {
		return nil, sharedmodel.WrapErr("validate list reviewable orders", err)
	}

	orders, err := b.storage.Querier().ListSuccessOrdersBySkus(ctx, orderdb.ListSuccessOrdersBySkusParams{
		BuyerID: params.AccountID,
		SkuIds:  params.SkuIDs,
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db list reviewable orders", err)
	}

	result := make([]ReviewableOrder, len(orders))
	for i, o := range orders {
		result[i] = ReviewableOrder{
			ID:          o.ID,
			Total:       o.Total,
			DateCreated: o.DateCreated,
		}
	}
	return result, nil
}

// ValidateOrderForReview checks if a specific order is eligible for review.
func (b *OrderHandler) ValidateOrderForReview(ctx restate.Context, params ValidateOrderForReviewParams) (bool, error) {
	if err := validator.Validate(params); err != nil {
		return false, sharedmodel.WrapErr("validate order for review", err)
	}

	return b.storage.Querier().ValidateOrderForReview(ctx, orderdb.ValidateOrderForReviewParams{
		OrderID: params.OrderID,
		BuyerID: params.AccountID,
		SkuIds:  params.SkuIDs,
	})
}
