package orderbiz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"shopnexus-remastered/internal/module/shared/pgsqlc"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/infras/payment"
	"shopnexus-remastered/internal/infras/pubsub"
	"shopnexus-remastered/internal/infras/shipment"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type OrderBiz struct {
	storage     pgsqlc.Storage
	paymentMap  map[string]payment.Client  // map[paymentOption]payment.Client
	shipmentMap map[string]shipment.Client // map[shipmentOption]shipment.Client
	pubsub      pubsub.Client
	promotion   *promotionbiz.PromotionBiz
	common      *commonbiz.Commonbiz
}

func NewOrderBiz(
	storage pgsqlc.Storage,
	pubsub pubsub.Client,
	promotion *promotionbiz.PromotionBiz,
	common *commonbiz.Commonbiz,
) (*OrderBiz, error) {
	b := &OrderBiz{
		storage:   storage,
		pubsub:    pubsub.Group("order"),
		promotion: promotion,
		common:    common,
	}

	return b, errors.Join(
		b.SetupPaymentMap(),
		b.SetupShipmentMap(),
		b.SetupPubsub(),
	)
}

type GetOrderParams = struct {
	Account authmodel.AuthenticatedAccount
	OrderID int64 `validate:"required,min=1"`
}

func (b *OrderBiz) GetOrder(ctx context.Context, params GetOrderParams) (ordermodel.Order, error) {
	var zero ordermodel.Order

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	orders, err := b.ListOrders(ctx, ListOrdersParams{
		ID: []int64{params.OrderID},
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
	ID []int64 `validate:"dive,min=1"`
}

func (b *OrderBiz) ListOrders(ctx context.Context, params ListOrdersParams) (commonmodel.PaginateResult[ordermodel.Order], error) {
	var zero commonmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	total, err := b.storage.CountOrderBase(ctx, db.CountOrderBaseParams{
		ID: params.ID,
	})
	if err != nil {
		return zero, err
	}

	orders, err := b.storage.ListOrderBase(ctx, db.ListOrderBaseParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
		ID:     params.ID,
	})
	if err != nil {
		return zero, err
	}

	orderItems, err := b.storage.ListOrderItem(ctx, db.ListOrderItemParams{
		OrderID: lo.Map(orders, func(o db.OrderBase, _ int) int64 { return o.ID }),
	})
	if err != nil {
		return zero, err
	}
	orderItemsMap := lo.GroupByMap(orderItems, func(oi db.OrderItem) (int64, db.OrderItem) { return oi.OrderID, oi })

	return commonmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data: lo.Map(orders, func(o db.OrderBase, _ int) ordermodel.Order {
			return ordermodel.Order{
				ID:            o.ID,
				AccountID:     o.AccountID,
				PaymentOption: o.PaymentOption,
				PaymentStatus: o.PaymentStatus,
				Address:       o.Address,
				DateCreated:   o.DateCreated.Time,
				DateUpdated:   o.DateUpdated.Time,
				Items:         orderItemsMap[o.ID],
			}
		}),
	}, nil
}

type CreateOrderParams struct {
	Storage       pgsqlc.Storage
	Account       authmodel.AuthenticatedAccount
	Address       string     `validate:"required"`
	PaymentOption string     `validate:"required,min=1,max=50"`
	BuyNow        bool       `validate:"omitempty"`
	Skus          []OrderSku `validate:"required,min=1,dive"`
}

type OrderSku struct {
	SkuID          int64   `json:"sku_id"`
	Quantity       int64   `json:"quantity"`
	PromotionIDs   []int64 `json:"promotion_ids"` // Promotions from system, vendor // TODO: Not handled yet
	ShipmentOption string  `json:"shipment_option"`
	Note           string  `json:"note"`
}

type CreateOrderResult struct {
	Order       ordermodel.Order `json:"order"`
	RedirectUrl null.String      `json:"url"`
}

func (b *OrderBiz) CreateOrder(ctx context.Context, params CreateOrderParams) (CreateOrderResult, error) {
	var zero CreateOrderResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}
	if params.BuyNow && len(params.Skus) != 1 {
		return zero, fmt.Errorf("buy now only support single sku")
	}

	skuIDs := lo.Map(params.Skus, func(s OrderSku, _ int) int64 { return s.SkuID })
	orderSkuMap := lo.KeyBy(params.Skus, func(s OrderSku) int64 { return s.SkuID })

	var (
		redirectUrl null.String
		orderID     int64
		totalPrice  commonmodel.Concurrency
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		if !params.BuyNow {
			cartItems, err := txStorage.RemoveCheckoutItem(ctx, db.RemoveCheckoutItemParams{
				CartID: params.Account.ID,
				SkuID:  skuIDs,
			})
			if err != nil {
				return fmt.Errorf("failed to remove checkout items: %w", err)
			}
			if len(cartItems) != len(skuIDs) {
				return fmt.Errorf("some sku not found in cart")
			}
		}

		var reserveStockErr error
		txStorage.ReserveInventory(ctx, lo.Map(params.Skus, func(item OrderSku, _ int) db.ReserveInventoryParams {
			return db.ReserveInventoryParams{
				RefType: db.InventoryStockRefTypeProductSku,
				RefID:   item.SkuID,
				Amount:  item.Quantity,
			}
		})).Exec(func(_ int, err error) {
			if err != nil {
				reserveStockErr = err
			}
		})
		if reserveStockErr != nil {
			return fmt.Errorf("failed to reserve inventory: %w", reserveStockErr)
		}

		skus, err := txStorage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
			ID: skuIDs,
		})
		if err != nil {
			return fmt.Errorf("failed to list catalog product skus: %w", err)
		}
		if len(skus) != len(skuIDs) {
			return ordermodel.ErrOrderItemNotFound
		}
		skuMap := lo.KeyBy(skus, func(s db.CatalogProductSku) int64 { return s.ID })

		spus, err := txStorage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
			ID: lo.Map(skus, func(s db.CatalogProductSku, _ int) int64 { return s.SpuID }),
		})
		if err != nil {
			return fmt.Errorf("failed to list catalog product spu: %w", err)
		}
		spuMap := lo.KeyBy(spus, func(s db.CatalogProductSpu) int64 { return s.ID })

		priceMap, err := b.promotion.CalculatePromotedPrices(ctx, skus, spuMap)
		if err != nil {
			return fmt.Errorf("failed to calculate promoted prices: %w", err)
		}
		totalPrice = 0
		for _, skuID := range skuIDs {
			totalPrice += priceMap[skuID].Price.Mul(orderSkuMap[skuID].Quantity)
		}

		order, err := txStorage.CreateDefaultOrderBase(ctx, db.CreateDefaultOrderBaseParams{
			AccountID:     params.Account.ID,
			PaymentOption: params.PaymentOption,
			Address:       params.Address,
		})
		if err != nil {
			return fmt.Errorf("failed to create order base: %w", err)
		}
		orderID = order.ID

		contacts, err := txStorage.GetVendorAddressBySkuIDs(ctx, skuIDs)
		if err != nil {
			return fmt.Errorf("failed to get vendor address by sku ids: %w", err)
		}
		contactMap := lo.KeyBy(contacts, func(c db.GetVendorAddressBySkuIDsRow) int64 { return c.SkuID })

		var createShipmentArgs []db.CreateBatchOrderShipmentParams
		for _, orderSku := range params.Skus {
			contact, ok := contactMap[orderSku.SkuID]
			if !ok {
				return fmt.Errorf("missing vendor address for sku %d", orderSku.SkuID)
			}
			shipmentClient, ok := b.shipmentMap[orderSku.ShipmentOption]
			if !ok {
				return fmt.Errorf("unknown shipment option: %s", orderSku.ShipmentOption)
			}

			var packageDetails shipment.PackageDetails
			if err := sonic.Unmarshal([]byte(skuMap[orderSku.SkuID].PackageDetails), &packageDetails); err != nil {
				return fmt.Errorf("failed to unmarshal packaged size for sku %d: %w", orderSku.SkuID, err)
			}

			ship, err := shipmentClient.Quote(ctx, shipment.CreateParams{
				FromAddress: contact.Address,
				ToAddress:   params.Address,
				Package:     packageDetails,
			})
			if err != nil {
				return fmt.Errorf("failed to quote shipment: %w", err)
			}

			createShipmentArgs = append(createShipmentArgs, db.CreateBatchOrderShipmentParams{
				FromAddress:  contact.Address,
				ToAddress:    params.Address,
				Option:       orderSku.ShipmentOption,
				TrackingCode: pgutil.StringToPgText(""),
				LabelUrl:     pgutil.StringToPgText(""),
				Status:       db.OrderShipmentStatusPending,
				Cost:         int64(ship.Costs),
				DateEta:      pgutil.TimeToPgTimestamptz(ship.ETA),
				WeightGrams:  10,
				LengthCm:     10,
				WidthCm:      10,
				HeightCm:     10,
				DateCreated:  pgutil.TimeToPgTimestamptz(time.Now()),
			})
		}

		shipmentMap := make(map[int64]db.OrderShipment)
		var createShipmentErr error
		txStorage.CreateBatchOrderShipment(ctx, createShipmentArgs).QueryRow(func(index int, s db.OrderShipment, err error) {
			if err != nil {
				createShipmentErr = err
				return
			}
			shipmentMap[skuIDs[index]] = s
		})
		if createShipmentErr != nil {
			return fmt.Errorf("failed to create order shipments: %w", createShipmentErr)
		}

		var createOrderItemArgs []db.CreateBatchOrderItemParams
		for _, skuID := range skuIDs {
			shipment, ok := shipmentMap[skuID]
			if !ok {
				return fmt.Errorf("missing shipment for sku %d", skuID)
			}

			if skuMap[skuID].CanCombine {
				createOrderItemArgs = append(createOrderItemArgs, db.CreateBatchOrderItemParams{
					VendorID:   contactMap[skuID].VendorID,
					OrderID:    order.ID,
					SkuID:      skuID,
					Quantity:   orderSkuMap[skuID].Quantity,
					ShipmentID: shipment.ID,
					Note:       orderSkuMap[skuID].Note,
					Status:     db.CommonStatusPending,
				})
			} else {
				for i := int64(0); i < orderSkuMap[skuID].Quantity; i++ {
					createOrderItemArgs = append(createOrderItemArgs, db.CreateBatchOrderItemParams{
						VendorID:   contactMap[skuID].VendorID,
						OrderID:    order.ID,
						SkuID:      skuID,
						Quantity:   1,
						ShipmentID: shipment.ID,
						Note:       orderSkuMap[skuID].Note,
						Status:     db.CommonStatusPending,
					})
				}
			}
		}

		var getProductArgs []db.GetAvailableProductsParams
		for _, skuID := range skuIDs {
			getProductArgs = append(getProductArgs, db.GetAvailableProductsParams{
				SkuID:  skuID,
				Amount: int32(orderSkuMap[skuID].Quantity),
			})
		}

		serialsMap := make(map[int64][]db.GetAvailableProductsRow)
		var getSerialsError error
		txStorage.GetAvailableProducts(ctx, getProductArgs).Query(func(i int, rows []db.GetAvailableProductsRow, err error) {
			if err != nil {
				getSerialsError = err
				return
			}
			if int32(len(rows)) < getProductArgs[i].Amount {
				skuID := getProductArgs[i].SkuID
				spuName := spuMap[skuMap[skuID].SpuID].Name
				getSerialsError = ordermodel.ErrOutOfStock.Fmt(fmt.Sprintf("%s (%d)", spuName, skuID))
				return
			}
			serialsMap[rows[0].SkuID] = rows
		})
		if getSerialsError != nil {
			return fmt.Errorf("failed to get available product serials: %w", getSerialsError)
		}

		var (
			batchErr              error
			serialIDs             []int64
			createOrderSerialArgs []db.CreateCopyDefaultOrderItemSerialParams
		)

		txStorage.CreateBatchOrderItem(ctx, createOrderItemArgs).QueryRow(func(_ int, orderItem db.OrderItem, err error) {
			if err != nil {
				batchErr = err
				return
			}

			for i := int64(0); i < orderItem.Quantity; i++ {
				if len(serialsMap[orderItem.SkuID]) == 0 {
					spu, err := txStorage.GetCatalogProductSpu(ctx, db.GetCatalogProductSpuParams{
						ID: pgutil.Int64ToPgInt8(skuMap[orderItem.SkuID].SpuID),
					})
					if err != nil {
						batchErr = err
						return
					}
					batchErr = ordermodel.ErrOutOfStock.Fmt(fmt.Sprintf("%s (%d)", spu.Name, orderItem.SkuID))
					return
				}

				serial := serialsMap[orderItem.SkuID][0]
				serialsMap[orderItem.SkuID] = serialsMap[orderItem.SkuID][1:]

				serialIDs = append(serialIDs, serial.ID)
				createOrderSerialArgs = append(createOrderSerialArgs, db.CreateCopyDefaultOrderItemSerialParams{
					OrderItemID:     orderItem.ID,
					ProductSerialID: serial.ID,
				})
			}
		})
		if batchErr != nil {
			return fmt.Errorf("failed to create order items: %w", batchErr)
		}

		if _, err := txStorage.CreateCopyDefaultOrderItemSerial(ctx, createOrderSerialArgs); err != nil {
			return fmt.Errorf("failed to attach order item serials: %w", err)
		}

		if err := txStorage.UpdateSerialStatus(ctx, db.UpdateSerialStatusParams{
			Status: db.InventoryProductStatusSold,
			ID:     serialIDs,
		}); err != nil {
			return fmt.Errorf("failed to update serial status: %w", err)
		}

		gateway, ok := b.paymentMap[params.PaymentOption]
		if !ok {
			return ordermodel.ErrPaymentGatewayNotFound
		}

		result, err := gateway.CreateOrder(ctx, payment.CreateOrderParams{
			RefID:  order.ID,
			Amount: totalPrice,
			Info:   fmt.Sprintf("Order #%d", order.ID),
		})
		if err != nil {
			return fmt.Errorf("failed to create payment order: %w", err)
		}
		if result.RedirectURL != "" {
			redirectUrl.SetValid(result.RedirectURL)
		}

		if err := b.pubsub.Publish(ordermodel.TopicOrderCreated, OrderCreatedParams{OrderID: order.ID}); err != nil {
			return fmt.Errorf("failed to publish order created event: %w", err)
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create order: %w", err)
	}

	newOrder, err := b.GetOrder(ctx, GetOrderParams{OrderID: orderID})
	if err != nil {
		return zero, fmt.Errorf("failed to fetch created order: %w", err)
	}

	return CreateOrderResult{Order: newOrder, RedirectUrl: redirectUrl}, nil
}

type CancelOrderParams = struct {
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount
	OrderID int64
}

func (b *OrderBiz) CancelOrder(ctx context.Context, params CancelOrderParams) error {
	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		payment, err := txStorage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(params.OrderID))
		if err != nil {
			return fmt.Errorf("failed to fetch order base: %w", err)
		}

		if payment.PaymentStatus != db.CommonStatusPending {
			return fmt.Errorf("payment %d cannot be canceled", params.OrderID)
		}

		if _, err := txStorage.UpdateOrderBase(ctx, db.UpdateOrderBaseParams{
			ID:            params.OrderID,
			PaymentStatus: db.NullCommonStatus{CommonStatus: db.CommonStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("failed to update order status: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to cancel order %d: %w", params.OrderID, err)
	}

	return nil
}

type VerifyPaymentParams struct {
	PaymentGateway string `validate:"required,min=1,max=50"`
	Data           map[string]any
}

func (b *OrderBiz) VerifyPayment(ctx context.Context, params VerifyPaymentParams) error {
	var refID int64

	if err := validator.Validate(params); err != nil {
		return err
	}

	// Verify payment via payment gateway
	if gateway, ok := b.paymentMap[params.PaymentGateway]; ok {
		result, err := gateway.VerifyPayment(ctx, params.Data)
		if err != nil {
			return err
		}
		refID = result.RefID
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
	Storage pgsqlc.Storage
	Account authmodel.AuthenticatedAccount
	Address string     `validate:"required"`
	Skus    []OrderSku `validate:"required,min=1,dive"`
}
type QuoteOrderResult struct {
	Subtotal commonmodel.Concurrency `json:"subtotal"`
	Shipping commonmodel.Concurrency `json:"shipping"`
}

func (b *OrderBiz) QuoteOrder(ctx context.Context, params QuoteOrderParams) (QuoteOrderResult, error) {
	var zero QuoteOrderResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	skuIDs := lo.Map(params.Skus, func(s OrderSku, _ int) int64 { return s.SkuID })
	orderSkuMap := lo.KeyBy(params.Skus, func(s OrderSku) int64 { return s.SkuID })

	var subtotal, shippingPrice commonmodel.Concurrency

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		skus, err := txStorage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
			ID: skuIDs,
		})
		if err != nil {
			return fmt.Errorf("failed to list catalog product skus: %w", err)
		}
		if len(skus) != len(skuIDs) {
			return ordermodel.ErrOrderItemNotFound
		}
		skuMap := lo.KeyBy(skus, func(s db.CatalogProductSku) int64 { return s.ID })

		spus, err := txStorage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
			ID: lo.Map(skus, func(s db.CatalogProductSku, _ int) int64 { return s.SpuID }),
		})
		if err != nil {
			return fmt.Errorf("failed to list catalog product spu: %w", err)
		}
		spuMap := lo.KeyBy(spus, func(s db.CatalogProductSpu) int64 { return s.ID })

		priceMap, err := b.promotion.CalculatePromotedPrices(ctx, skus, spuMap)
		if err != nil {
			return fmt.Errorf("failed to calculate promoted prices: %w", err)
		}

		for _, sku := range params.Skus {
			subtotal += priceMap[sku.SkuID].Price.Mul(orderSkuMap[sku.SkuID].Quantity)

			spu, ok := spuMap[skuMap[sku.SkuID].SpuID]
			if !ok {
				return fmt.Errorf("missing spu for sku %d", sku.SkuID)
			}

			vendorContact, err := b.getDefaultContact(ctx, spu.AccountID)
			if err != nil {
				return fmt.Errorf("failed to get vendor contact: %w", err)
			}

			shipmentClient, ok := b.shipmentMap[sku.ShipmentOption]
			if !ok {
				return fmt.Errorf("unknown shipment option: %s", sku.ShipmentOption)
			}

			var packageDetails shipment.PackageDetails
			if err := sonic.Unmarshal([]byte(skuMap[sku.SkuID].PackageDetails), &packageDetails); err != nil {
				return fmt.Errorf("failed to unmarshal packaged size for sku %d: %w", sku.SkuID, err)
			}

			shipmentQuote, err := shipmentClient.Quote(ctx, shipment.CreateParams{
				FromAddress: vendorContact.Address,
				ToAddress:   params.Address,
				Package:     packageDetails,
			})
			if err != nil {
				return fmt.Errorf("failed to quote shipment: %w", err)
			}

			shippingPrice += shipmentQuote.Costs.Mul(orderSkuMap[sku.SkuID].Quantity)
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to quote order: %w", err)
	}

	return QuoteOrderResult{
		Subtotal: subtotal,
		Shipping: shippingPrice,
	}, nil
}

// TODO: should call the account biz instead of using storage directly
func (b *OrderBiz) getDefaultContact(ctx context.Context, accountID int64) (db.AccountContact, error) {
	var zero db.AccountContact

	profile, err := b.storage.GetAccountProfile(ctx, db.GetAccountProfileParams{
		ID: pgutil.Int64ToPgInt8(accountID),
	})
	if err != nil {
		return zero, err
	}

	contact, err := b.storage.GetAccountContact(ctx, profile.DefaultContactID)
	if err != nil {
		return zero, err
	}

	return contact, nil
}
