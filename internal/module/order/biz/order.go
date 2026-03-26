package orderbiz

import (
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/shipment"
	accountmodel "shopnexus-server/internal/module/account/model"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	commonbiz "shopnexus-server/internal/module/common/biz"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// GetOrder returns a single order by ID with all items and payment details.
func (b *OrderHandler) GetOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	var zero ordermodel.Order

	orders, err := b.ListOrders(ctx, ListOrdersParams{
		ID: []uuid.UUID{orderID},
	})
	if err != nil {
		return zero, err
	}
	if len(orders.Data) == 0 {
		return zero, ordermodel.ErrOrderNotFound.Terminal()
	}

	return orders.Data[0], nil
}

type ListOrdersParams struct {
	sharedmodel.PaginationParams
	ID []uuid.UUID `validate:"dive"`
}

// ListOrders returns paginated orders with hydrated items, payments, and product resources.
func (b *OrderHandler) ListOrders(ctx restate.Context, params ListOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountOrderRow, error) {
		return b.storage.Querier().ListCountOrder(ctx, orderdb.ListCountOrderParams{
			Limit:  params.Limit,
			Offset: params.Offset(),
			ID:     params.ID,
		})
	})
	if err != nil {
		return zero, err
	}

	var total null.Int
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders := lo.Map(listCountOrder, func(item orderdb.ListCountOrderRow, _ int) orderdb.OrderOrder {
		return item.OrderOrder
	})
	data, err := b.hydrateOrders(ctx, orders)
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       data,
	}, nil
}

func (b *OrderHandler) hydrateOrders(ctx restate.Context, orders []orderdb.OrderOrder) ([]ordermodel.Order, error) {
	if len(orders) == 0 {
		return []ordermodel.Order{}, nil
	}

	orderIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) uuid.UUID { return o.ID })
	paymentIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) int64 { return o.PaymentID })

	// Fetch order items and payments from DB inside Run
	type dbResults struct {
		OrderItems []orderdb.OrderItem    `json:"order_items"`
		Payments   []orderdb.OrderPayment `json:"payments"`
	}
	dbData, err := restate.Run(ctx, func(ctx restate.RunContext) (dbResults, error) {
		orderItems, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			OrderID: orderIDs,
		})
		if err != nil {
			return dbResults{}, err
		}

		payments, err := b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
			ID: paymentIDs,
		})
		if err != nil {
			return dbResults{}, err
		}

		return dbResults{OrderItems: orderItems, Payments: payments}, nil
	})
	if err != nil {
		return nil, err
	}

	orderItemsMap := lo.GroupByMap(dbData.OrderItems, func(oi orderdb.OrderItem) (uuid.UUID, orderdb.OrderItem) { return oi.OrderID, oi })

	// Lookup SKU → SPU to get product images (cross-module call, needs restate.Context)
	skuIDs := lo.Map(dbData.OrderItems, func(oi orderdb.OrderItem, _ int) uuid.UUID { return oi.SkuID })
	skuIDs = lo.Uniq(skuIDs)

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

	// GetResources uses context.Context, safe to call with restate.Context (which embeds it)
	resourcesMap, err := b.common.GetResources(ctx, commonbiz.GetResourcesParams{
		RefType: commondb.CommonResourceRefTypeProductSpu,
		RefIDs:  spuIDs,
	})
	if err != nil {
		return nil, err
	}

	// Build enriched items map: orderID -> []OrderItem
	enrichedItemsMap := make(map[uuid.UUID][]ordermodel.OrderItem)
	for orderID, items := range orderItemsMap {
		enriched := make([]ordermodel.OrderItem, 0, len(items))
		for _, oi := range items {
			spuID := skuToSpuMap[oi.SkuID]
			enriched = append(enriched, ordermodel.OrderItem{
				ID:        oi.ID,
				OrderID:   oi.OrderID,
				SkuID:     oi.SkuID,
				SkuName:   oi.SkuName,
				Quantity:  oi.Quantity,
				UnitPrice: oi.UnitPrice,
				Note:      oi.Note,
				SerialIds: oi.SerialIds,
				Resources: resourcesMap[spuID],
			})
		}
		enrichedItemsMap[orderID] = enriched
	}

	paymentMap := lo.KeyBy(dbData.Payments, func(p orderdb.OrderPayment) int64 { return p.ID })

	result := make([]ordermodel.Order, 0, len(orders))
	for _, o := range orders {
		payment, ok := paymentMap[o.PaymentID]
		if !ok {
			return nil, ordermodel.ErrMissingPayment.Terminal()
		}

		result = append(result, ordermodel.Order{
			ID:         o.ID,
			CustomerID: o.CustomerID,
			VendorID:   o.VendorID,
			ShipmentID: o.ShipmentID,
			Payment: ordermodel.Payment{
				ID:          payment.ID,
				AccountID:   payment.AccountID,
				Option:      payment.Option,
				Status:      payment.Status,
				Amount:      sharedmodel.Concurrency(payment.Amount),
				Data:        payment.Data,
				DateCreated: payment.DateCreated,
				DatePaid:    payment.DatePaid,
				DateExpired: payment.DateExpired,
			},
			Status:          o.Status,
			Address:         o.Address,
			ProductCost:     sharedmodel.Concurrency(o.ProductCost),
			ShipCost:        sharedmodel.Concurrency(o.ShipCost),
			ProductDiscount: sharedmodel.Concurrency(o.ProductDiscount),
			ShipDiscount:    sharedmodel.Concurrency(o.ShipDiscount),
			Total:           sharedmodel.Concurrency(o.Total),
			Note:            o.Note,
			Data:            o.Data,
			DateCreated:     o.DateCreated,
			Items:           enrichedItemsMap[o.ID],
		})
	}

	return result, nil
}

type VerifyPaymentParams struct {
	PaymentGateway string `validate:"required,min=1,max=50"`
	Data           map[string]any
}

// VerifyPayment verifies a payment callback from the payment gateway and updates the payment status.
func (b *OrderHandler) VerifyPayment(ctx restate.Context, params VerifyPaymentParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	// Verify payment via payment gateway
	refID, err := restate.Run(ctx, func(ctx restate.RunContext) (uuid.UUID, error) {
		gateway, ok := b.paymentMap[params.PaymentGateway]
		if !ok {
			return uuid.UUID{}, ordermodel.ErrPaymentGatewayNotFound.Terminal()
		}
		result, err := gateway.VerifyPayment(ctx, params.Data)
		if err != nil {
			return uuid.UUID{}, err
		}
		return uuid.Parse(result.RefID)
	})
	if err != nil {
		return err
	}

	// Update payment status
	return restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		order, err := b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: refID, Valid: true})
		if err != nil {
			return err
		}

		_, err = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
			ID:     order.PaymentID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusSuccess, Valid: true},
		})
		return err
	})
}

type QuoteOrderParams struct {
	Account accountmodel.AuthenticatedAccount
	Address string         `validate:"required"`
	Items   []CheckoutItem `validate:"required,min=1,dive"`
}
type QuoteOrderResult struct {
	ProductCost sharedmodel.Concurrency `json:"product_cost"`
	ShipCost    sharedmodel.Concurrency `json:"ship_cost"`
	Total       sharedmodel.Concurrency `json:"total"`
}

// QuoteOrder calculates the estimated total cost including shipping and promotions without placing an order.
func (b *OrderHandler) QuoteOrder(ctx restate.Context, params QuoteOrderParams) (QuoteOrderResult, error) {
	var zero QuoteOrderResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	skuIDs := lo.Map(params.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })

	// Get skus map (cross-module, needs restate.Context)
	quoteSkus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return zero, fmt.Errorf("list catalog product skus: %w", err)
	}
	if len(quoteSkus) != len(skuIDs) {
		return zero, ordermodel.ErrOrderItemNotFound.Terminal()
	}
	skuMap := lo.KeyBy(quoteSkus, func(s catalogmodel.ProductSku) uuid.UUID { return s.ID })

	// Get spus map (cross-module, needs restate.Context)
	quoteSpus, err := b.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{
		ID: lo.Map(quoteSkus, func(s catalogmodel.ProductSku, _ int) uuid.UUID { return s.SpuID }),
	})
	if err != nil {
		return zero, fmt.Errorf("list catalog product spu: %w", err)
	}
	spuMap := lo.KeyBy(quoteSpus.Data, func(s catalogmodel.ProductSpu) uuid.UUID { return s.ID })

	vendorIDs := lo.Map(quoteSpus.Data, func(s catalogmodel.ProductSpu, _ int) uuid.UUID { return s.AccountID })
	contactMap, err := b.account.GetDefaultContact(ctx, vendorIDs)
	if err != nil {
		return zero, fmt.Errorf("get default contact map: %w", err)
	}

	return restate.Run(ctx, func(ctx restate.RunContext) (QuoteOrderResult, error) {
		// Quote shipping per checkout item
		shippingCostMap := make(map[uuid.UUID]sharedmodel.Concurrency, len(params.Items))
		for _, checkoutItem := range params.Items {
			shipmentClient, err := b.getShipmentClient(checkoutItem.ShipmentOption)
			if err != nil {
				return zero, fmt.Errorf("get shipment client: %w", err)
			}

			sku := skuMap[checkoutItem.SkuID]
			contact := contactMap[spuMap[sku.SpuID].AccountID]

			var packageDetails shipment.PackageDetails
			if err := sonic.Unmarshal(sku.PackageDetails, &packageDetails); err != nil {
				return zero, fmt.Errorf("unmarshal package details for sku %s: %w", checkoutItem.SkuID, err)
			}

			shipmentQuote, err := shipmentClient.Quote(ctx, shipment.CreateParams{
				FromAddress: contact.Address,
				ToAddress:   params.Address,
				Package:     packageDetails,
			})
			if err != nil {
				return zero, fmt.Errorf("quote shipment: %w", err)
			}

			shippingCostMap[checkoutItem.SkuID] = sharedmodel.Concurrency(shipmentQuote.Costs)
		}

		// Calculate promoted prices with quoted shipping
		requestOrderPrices := make([]catalogmodel.RequestOrderPrice, 0, len(params.Items))
		for _, orderItem := range params.Items {
			shipCost, ok := shippingCostMap[orderItem.SkuID]
			if !ok {
				return zero, ordermodel.ErrMissingShippingQuote.Terminal()
			}

			requestOrderPrices = append(requestOrderPrices, catalogmodel.RequestOrderPrice{
				SkuID:          orderItem.SkuID,
				SpuID:          skuMap[orderItem.SkuID].SpuID,
				UnitPrice:      skuMap[orderItem.SkuID].Price,
				Quantity:       orderItem.Quantity,
				ShipCost:       shipCost,
				PromotionCodes: orderItem.PromotionCodes,
			})
		}

		priceMap, err := b.promotion.CalculatePromotedPrices(ctx, promotionbiz.CalculatePromotedPricesParams{Prices: requestOrderPrices, SpuMap: spuMap})
		if err != nil {
			return zero, fmt.Errorf("calculate promoted prices: %w", err)
		}

		var result QuoteOrderResult
		for _, checkoutItem := range params.Items {
			price, ok := priceMap[checkoutItem.SkuID]
			if !ok {
				return zero, ordermodel.ErrMissingPromotedPrice.Terminal()
			}

			result.ProductCost = result.ProductCost.Add(price.ProductCost)
			result.ShipCost = result.ShipCost.Add(price.ShipCost)
		}
		result.Total = result.ProductCost.Add(result.ShipCost)

		return result, nil
	})
}
