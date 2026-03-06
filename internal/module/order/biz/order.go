package orderbiz

import (
	"context"
	"errors"
	"fmt"

	"shopnexus-remastered/internal/infras/payment"
	"shopnexus-remastered/internal/infras/pubsub"
	"shopnexus-remastered/internal/infras/shipment"
	accountbiz "shopnexus-remastered/internal/module/account/biz"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	analyticbiz "shopnexus-remastered/internal/module/analytic/biz"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/pgsqlc"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type OrderStorage = pgsqlc.Storage[*orderdb.Queries]

type OrderBiz struct {
	storage     OrderStorage
	paymentMap  map[string]payment.Client  // map[paymentOption]payment.Client
	shipmentMap map[string]shipment.Client // map[shipmentOption]shipment.Client
	pubsub      pubsub.Client
	account     *accountbiz.AccountBiz
	catalog     *catalogbiz.CatalogBiz
	inventory   *inventorybiz.InventoryBiz
	promotion   *promotionbiz.PromotionBiz
	common      *commonbiz.CommonBiz
	analytic    *analyticbiz.AnalyticBiz
}

func NewOrderBiz(
	storage OrderStorage,
	pubsub pubsub.Client,
	account *accountbiz.AccountBiz,
	catalog *catalogbiz.CatalogBiz,
	inventory *inventorybiz.InventoryBiz,
	promotion *promotionbiz.PromotionBiz,
	common *commonbiz.CommonBiz,
	analytic *analyticbiz.AnalyticBiz,
) (*OrderBiz, error) {
	b := &OrderBiz{
		storage:   storage,
		pubsub:    pubsub.Group("order"),
		account:   account,
		catalog:   catalog,
		inventory: inventory,
		promotion: promotion,
		common:    common,
		analytic:  analytic,
	}

	return b, errors.Join(
		b.SetupPaymentMap(),
		b.SetupShipmentMap(),
		b.SetupPubsub(),
	)
}

func (b *OrderBiz) GetOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	var zero ordermodel.Order

	orders, err := b.ListOrders(ctx, ListOrdersParams{
		ID: []uuid.UUID{orderID},
	})
	if err != nil {
		return zero, err
	}
	if len(orders.Data) == 0 {
		return zero, fmt.Errorf("order not found")
	}

	return orders.Data[0], nil
}

type ListOrdersParams struct {
	commonmodel.PaginationParams
	ID []uuid.UUID `validate:"dive"`
}

func (b *OrderBiz) ListOrders(ctx context.Context, params ListOrdersParams) (commonmodel.PaginateResult[ordermodel.Order], error) {
	var zero commonmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	listCountOrder, err := b.storage.Querier().ListCountOrder(ctx, orderdb.ListCountOrderParams{
		Limit:  params.Limit,
		Offset: params.Offset(),
		ID:     params.ID,
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

	return commonmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       data,
	}, nil
}

func (b *OrderBiz) hydrateOrders(ctx context.Context, orders []orderdb.OrderOrder) ([]ordermodel.Order, error) {
	if len(orders) == 0 {
		return []ordermodel.Order{}, nil
	}

	orderIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) uuid.UUID { return o.ID })
	paymentIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) int64 { return o.PaymentID })

	orderItems, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
		OrderID: orderIDs,
	})
	if err != nil {
		return nil, err
	}
	orderItemsMap := lo.GroupByMap(orderItems, func(oi orderdb.OrderItem) (uuid.UUID, orderdb.OrderItem) { return oi.OrderID, oi })

	payments, err := b.storage.Querier().ListPayment(ctx, orderdb.ListPaymentParams{
		ID: paymentIDs,
	})
	if err != nil {
		return nil, err
	}
	paymentMap := lo.KeyBy(payments, func(p orderdb.OrderPayment) int64 { return p.ID })

	result := make([]ordermodel.Order, 0, len(orders))
	for _, o := range orders {
		payment, ok := paymentMap[o.PaymentID]
		if !ok {
			return nil, fmt.Errorf("missing payment %d for order %s", o.PaymentID, o.ID)
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
				Amount:      commonmodel.Concurrency(payment.Amount),
				Data:        payment.Data,
				DateCreated: payment.DateCreated,
				DatePaid:    payment.DatePaid,
				DateExpired: payment.DateExpired,
			},
			Status:          o.Status,
			Address:         o.Address,
			ProductCost:     commonmodel.Concurrency(o.ProductCost),
			ShipCost:        commonmodel.Concurrency(o.ShipCost),
			ProductDiscount: commonmodel.Concurrency(o.ProductDiscount),
			ShipDiscount:    commonmodel.Concurrency(o.ShipDiscount),
			Total:           commonmodel.Concurrency(o.Total),
			Note:            o.Note,
			Data:            o.Data,
			DateCreated:     o.DateCreated,
			Items:           orderItemsMap[o.ID],
		})
	}

	return result, nil
}

type VerifyPaymentParams struct {
	PaymentGateway string `validate:"required,min=1,max=50"`
	Data           map[string]any
}

func (b *OrderBiz) VerifyPayment(ctx context.Context, params VerifyPaymentParams) error {
	var refID uuid.UUID

	if err := validator.Validate(params); err != nil {
		return err
	}

	// Verify payment via payment gateway
	if gateway, ok := b.paymentMap[params.PaymentGateway]; ok {
		result, err := gateway.VerifyPayment(ctx, params.Data)
		if err != nil {
			return err
		}
		refID, err = uuid.Parse(result.RefID)
		if err != nil {
			return err
		}
	} else {
		return ordermodel.ErrPaymentGatewayNotFound
	}

	// Publish event for order paid
	if err := b.pubsub.Publish(ordermodel.TopicOrderPaid, OrderPaidParams{
		OrderID: refID,
	}); err != nil {
		return err
	}

	return nil
}

type QuoteOrderParams struct {
	Storage OrderStorage
	Account accountmodel.AuthenticatedAccount
	Address string         `validate:"required"`
	Items   []CheckoutItem `validate:"required,min=1,dive"`
}
type QuoteOrderResult struct {
	ProductCost commonmodel.Concurrency `json:"product_cost"`
	ShipCost    commonmodel.Concurrency `json:"ship_cost"`
	Total       commonmodel.Concurrency `json:"total"`
}

func (b *OrderBiz) QuoteOrder(ctx context.Context, params QuoteOrderParams) (QuoteOrderResult, error) {
	var zero QuoteOrderResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// Get skus map
	skuIDs := lo.Map(params.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return zero, fmt.Errorf("failed to list catalog product skus: %w", err)
	}
	if len(skus) != len(skuIDs) {
		return zero, ordermodel.ErrOrderItemNotFound
	}
	skuMap := lo.KeyBy(skus, func(s catalogmodel.ProductSku) uuid.UUID { return s.ID })

	// Get spus map
	spus, err := b.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{
		ID: lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID { return s.SpuID }),
	})
	if err != nil {
		return zero, fmt.Errorf("failed to list catalog product spu: %w", err)
	}
	spuMap := lo.KeyBy(spus.Data, func(s catalogmodel.ProductSpu) uuid.UUID { return s.ID })

	vendorIDs := lo.Map(spus.Data, func(s catalogmodel.ProductSpu, _ int) uuid.UUID { return s.AccountID })
	contactMap, err := b.account.GetDefaultContact(ctx, vendorIDs)
	if err != nil {
		return zero, fmt.Errorf("failed to get default contact map: %w", err)
	}

	// Quote shipping per checkout item
	shippingCostMap := make(map[uuid.UUID]commonmodel.Concurrency, len(params.Items))
	for _, checkoutItem := range params.Items {
		shipmentClient, err := b.getShipmentClient(checkoutItem.ShipmentOption)
		if err != nil {
			return zero, fmt.Errorf("failed to get shipment client: %w", err)
		}

		sku := skuMap[checkoutItem.SkuID]
		contact := contactMap[spuMap[sku.SpuID].AccountID]

		var packageDetails shipment.PackageDetails
		if err := sonic.Unmarshal(sku.PackageDetails, &packageDetails); err != nil {
			return zero, fmt.Errorf("failed to unmarshal package details for sku %s: %w", checkoutItem.SkuID, err)
		}

		shipmentQuote, err := shipmentClient.Quote(ctx, shipment.CreateParams{
			FromAddress: contact.Address,
			ToAddress:   params.Address,
			Package:     packageDetails,
		})
		if err != nil {
			return zero, fmt.Errorf("failed to quote shipment: %w", err)
		}

		shippingCostMap[checkoutItem.SkuID] = commonmodel.Concurrency(shipmentQuote.Costs)
	}

	// Calculate promoted prices with quoted shipping
	requestOrderPrices := make([]catalogmodel.RequestOrderPrice, 0, len(params.Items))
	for _, orderItem := range params.Items {
		shipCost, ok := shippingCostMap[orderItem.SkuID]
		if !ok {
			return zero, fmt.Errorf("missing shipping quote for sku %s", orderItem.SkuID)
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

	priceMap, err := b.promotion.CalculatePromotedPrices(ctx, requestOrderPrices, spuMap)
	if err != nil {
		return zero, fmt.Errorf("failed to calculate promoted prices: %w", err)
	}

	var result QuoteOrderResult
	for _, checkoutItem := range params.Items {
		price, ok := priceMap[checkoutItem.SkuID]
		if !ok {
			return zero, fmt.Errorf("missing promoted price for sku %s", checkoutItem.SkuID)
		}

		result.ProductCost = result.ProductCost.Add(price.ProductCost)
		result.ShipCost = result.ShipCost.Add(price.ShipCost)
	}
	result.Total = result.ProductCost.Add(result.ShipCost)

	return result, nil
}
