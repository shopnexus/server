package orderbiz

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/metrics"
	accountbiz "shopnexus-server/internal/module/account/biz"
	accountmodel "shopnexus-server/internal/module/account/model"
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
// TODO: add casbin authorization — verify caller owns this order
func (b *OrderHandler) GetBuyerOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	var zero ordermodel.Order

	order, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderOrder, error) {
		return b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: orderID, Valid: true})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get order", err)
	}

	hydrated, err := b.hydrateOrders(ctx, []orderdb.OrderOrder{order})
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate order", err)
	}
	if len(hydrated) == 0 {
		return zero, ordermodel.ErrOrderNotFound.Terminal()
	}

	return hydrated[0], nil
}

// GetSellerOrder returns a single order by ID (seller perspective).
// TODO: add casbin authorization — verify caller is this order's seller
func (b *OrderHandler) GetSellerOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	return b.GetBuyerOrder(ctx, orderID)
}

// ListBuyerConfirmed returns paginated orders with hydrated items, payments, and product resources.
func (b *OrderHandler) ListBuyerConfirmed(
	ctx restate.Context,
	params ListBuyerConfirmedParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list orders", err)
	}

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountBuyerOrderRow, error) {
		return b.storage.Querier().ListCountBuyerOrder(ctx, orderdb.ListCountBuyerOrderParams{
			BuyerID: params.BuyerID,
			Limit:   params.Limit,
			Offset:  params.Offset(),
		})
	})

	if err != nil {
		return zero, sharedmodel.WrapErr("db list orders", err)
	}

	var total null.Int64
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders := lo.Map(listCountOrder, func(item orderdb.ListCountBuyerOrderRow, _ int) orderdb.OrderOrder {
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
func (b *OrderHandler) ListSellerConfirmed(
	ctx restate.Context,
	params ListSellerConfirmedParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list seller orders", err)
	}

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountSellerOrderRow, error) {
		return b.storage.Querier().ListCountSellerOrder(ctx, orderdb.ListCountSellerOrderParams{
			SellerID: params.SellerID,
			Search:   params.Search,
			Offset:   params.Offset(),
			Limit:    params.Limit,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list seller orders", err)
	}

	var total null.Int64
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders, err := b.hydrateOrders(
		ctx,
		lo.Map(listCountOrder, func(item orderdb.ListCountSellerOrderRow, _ int) orderdb.OrderOrder {
			return item.OrderOrder
		}),
	)
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

	// Collect transport IDs (only valid ones)
	var transportIDs []uuid.UUID
	for _, o := range orders {
		if o.TransportID.Valid {
			transportIDs = append(transportIDs, o.TransportID.UUID)
		}
	}

	// Fetch order items and transports from DB inside Run
	type dbResults struct {
		OrderItems []orderdb.OrderItem      `json:"order_items"`
		Transports []orderdb.OrderTransport `json:"transports"`
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

		var transports []orderdb.OrderTransport
		if len(transportIDs) > 0 {
			transports, err = b.storage.Querier().ListTransport(ctx, orderdb.ListTransportParams{
				ID: transportIDs,
			})
			if err != nil {
				return dbResults{}, err
			}
		}

		return dbResults{OrderItems: orderItems, Transports: transports}, nil
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db fetch order data", err)
	}

	// Collect payment IDs from items (orders no longer have payment_id)
	paymentIDSet := make(map[int64]struct{})
	orderPaymentMap := make(map[uuid.UUID]int64) // order_id -> payment_id (from first item)
	for _, item := range dbData.OrderItems {
		if item.OrderID.Valid && item.PaymentID.Valid {
			if _, exists := orderPaymentMap[item.OrderID.UUID]; !exists {
				orderPaymentMap[item.OrderID.UUID] = item.PaymentID.Int64
			}
			paymentIDSet[item.PaymentID.Int64] = struct{}{}
		}
	}
	paymentIDs := lo.Keys(paymentIDSet)

	// Fetch payments
	var payments []orderdb.OrderPayment
	if len(paymentIDs) > 0 {
		payments, err = restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderPayment, error) {
			return b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
				ID: paymentIDs,
			})
		})
		if err != nil {
			return nil, sharedmodel.WrapErr("db fetch payments", err)
		}
	}

	// Enrich all items in one batch (single ListProductSku + GetResources call)
	allEnriched, err := b.enrichItems(ctx, dbData.OrderItems)
	if err != nil {
		return nil, sharedmodel.WrapErr("enrich order items", err)
	}

	// Group enriched items by order_id
	enrichedItemsMap := make(map[uuid.UUID][]ordermodel.OrderItem)
	for _, item := range allEnriched {
		if item.OrderID != nil {
			enrichedItemsMap[*item.OrderID] = append(enrichedItemsMap[*item.OrderID], item)
		}
	}

	paymentMap := lo.KeyBy(payments, func(p orderdb.OrderPayment) int64 { return p.ID })
	transportMap := lo.KeyBy(dbData.Transports, func(t orderdb.OrderTransport) uuid.UUID { return t.ID })

	result := make([]ordermodel.Order, 0, len(orders))
	for _, o := range orders {
		var paymentPtr *ordermodel.Payment
		if pid, ok := orderPaymentMap[o.ID]; ok {
			if p, found := paymentMap[pid]; found {
				pm := dbToPayment(p)
				paymentPtr = &pm
			}
		}

		var transportPtr *ordermodel.Transport
		if o.TransportID.Valid {
			if t, ok := transportMap[o.TransportID.UUID]; ok {
				tr := dbToTransport(t)
				transportPtr = &tr
			}
		}

		result = append(result, ordermodel.Order{
			ID:              o.ID,
			BuyerID:         o.BuyerID,
			SellerID:        o.SellerID,
			Transport:       transportPtr,
			Payment:         paymentPtr,
			Address:         o.Address,
			ProductCost:     o.ProductCost,
			ProductDiscount: o.ProductDiscount,
			TransportCost:   o.TransportCost,
			Total:           o.Total,
			Note:            o.Note,
			Data:            o.Data,
			DateCreated:     o.DateCreated,
			Items:           enrichedItemsMap[o.ID],
		})
	}

	return result, nil
}

// dbToPayment maps a DB OrderPayment row to the model type.
func dbToPayment(p orderdb.OrderPayment) ordermodel.Payment {
	var datePaid *time.Time
	if p.DatePaid.Valid {
		datePaid = &p.DatePaid.Time
	}
	var pmID *uuid.UUID
	if p.PaymentMethodID.Valid {
		pmID = &p.PaymentMethodID.UUID
	}
	return ordermodel.Payment{
		ID:              p.ID,
		AccountID:       p.AccountID,
		Option:          p.Option,
		PaymentMethodID: pmID,
		Status:          p.Status,
		Amount:          p.Amount,
		Data:            p.Data,
		DateCreated:     p.DateCreated,
		DatePaid:        datePaid,
		DateExpired:     p.DateExpired,
	}
}

// dbToTransport maps a DB OrderTransport row to the model type.
func dbToTransport(t orderdb.OrderTransport) ordermodel.Transport {
	var dateCreated time.Time
	if t.DateCreated.Valid {
		dateCreated = t.DateCreated.Time
	}
	return ordermodel.Transport{
		ID:          t.ID,
		Option:      t.Option,
		Status:      t.Status.OrderTransportStatus,
		Cost:        t.Cost,
		Data:        t.Data,
		DateCreated: dateCreated,
	}
}

// ConfirmPayment updates the payment status based on a webhook callback result.
// Called by the transport layer after provider-specific webhook verification.
// RefID is the payment ID (int64 serialized as string).
func (b *OrderHandler) ConfirmPayment(ctx restate.Context, params ConfirmPaymentParams) (err error) {
	defer metrics.TrackHandler("order", "ConfirmPayment", &err)()

	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate confirm payment", err)
	}

	paymentID, err := strconv.ParseInt(params.RefID, 10, 64)
	if err != nil {
		return sharedmodel.WrapErr("parse payment ref id", err)
	}

	// Distributed lock per payment — prevents race with CancelUnpaidCheckout
	unlock := b.locker.Lock(ctx, fmt.Sprintf("order:payment:%d", paymentID))
	defer unlock()

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
		updated, err := b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
			ID:     paymentID,
			Status: orderdb.NullOrderStatus{OrderStatus: dbStatus, Valid: true},
		})
		if err != nil {
			return confirmResult{}, err
		}

		// Extract redirect_url from payment data for notification metadata
		var redirectURL string
		var data struct {
			RedirectURL string `json:"redirect_url"`
		}
		_ = json.Unmarshal(updated.Data, &data)
		redirectURL = data.RedirectURL

		return confirmResult{BuyerID: updated.AccountID.String(), RedirectURL: redirectURL}, nil
	})
	if err != nil {
		return err
	}

	// Record payment metric
	metrics.PaymentsTotal.WithLabelValues(string(params.Status), "webhook").Inc()

	// Notify buyer about payment result
	buyerID, _ := uuid.Parse(cr.BuyerID)
	switch params.Status {
	case payment.StatusSuccess:
		meta, _ := json.Marshal(map[string]string{"payment_id": strconv.FormatInt(paymentID, 10)})
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: buyerID,
			Type:      accountmodel.NotiPaymentSuccess,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Payment successful",
			Content:   "Your payment has been confirmed successfully.",
			Metadata:  meta,
		})

		// Start 48h seller timeout — auto-cancel pending items if seller doesn't confirm
		restate.ServiceSend(ctx, "Order", "AutoCancelPendingItems").
			Send(paymentID, restate.WithDelay(48*time.Hour))

	case payment.StatusFailed:
		metaMap := map[string]string{"payment_id": strconv.FormatInt(paymentID, 10)}
		if cr.RedirectURL != "" {
			metaMap["redirect_url"] = cr.RedirectURL
		}
		meta, _ := json.Marshal(metaMap)
		restate.ServiceSend(ctx, "Account", "CreateNotification").Send(accountbiz.CreateNotificationParams{
			AccountID: buyerID,
			Type:      accountmodel.NotiPaymentFailed,
			Channel:   accountmodel.ChannelInApp,
			Title:     "Payment failed",
			Content:   "Your payment could not be processed. Please try again.",
			Metadata:  meta,
		})
	}

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
func (b *OrderHandler) ListReviewableOrders(
	ctx restate.Context,
	params ListReviewableOrdersParams,
) ([]ReviewableOrder, error) {
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
