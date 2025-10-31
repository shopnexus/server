package orderbiz

import (
	"context"
	"fmt"
	"time"

	"shopnexus-remastered/internal/utils/errutil"

	"shopnexus-remastered/internal/client/payment"
	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/client/shipment"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
	"shopnexus-remastered/internal/utils/slice"

	"github.com/guregu/null/v6"
)

type OrderBiz struct {
	storage     *pgutil.Storage
	paymentMap  map[string]payment.Client  // map[paymentOption]payment.Client
	shipmentMap map[string]shipment.Client // map[shipmentOption]shipment.Client
	pubsub      pubsub.Client
	promotion   *promotionbiz.PromotionBiz
	shared      *sharedbiz.SharedBiz
}

func NewOrderBiz(
	storage *pgutil.Storage,
	pubsub pubsub.Client,
	promotion *promotionbiz.PromotionBiz,
	shared *sharedbiz.SharedBiz,
) (*OrderBiz, error) {
	b := &OrderBiz{
		storage:   storage,
		pubsub:    pubsub.Group("order"),
		promotion: promotion,
		shared:    shared,
	}

	return b, errutil.Some(
		b.SetupPaymentMap(),
		b.SetupShipmentMap(),
		b.SetupPubsub(),
	)
}

type GetOrderParams = struct {
	Account authmodel.AuthenticatedAccount
	OrderID int64
}

func (s *OrderBiz) GetOrder(ctx context.Context, params GetOrderParams) (db.OrderBase, error) {
	return s.storage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(params.OrderID))
}

type ListOrdersParams struct {
	sharedmodel.PaginationParams
}

func (s *OrderBiz) ListOrders(ctx context.Context, params ListOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	total, err := s.storage.CountOrderBase(ctx, db.CountOrderBaseParams{})
	if err != nil {
		return zero, err
	}

	orders, err := s.storage.ListOrderBase(ctx, db.ListOrderBaseParams{
		Limit:  pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset: pgutil.Int32ToPgInt4(params.Offset()),
	})
	if err != nil {
		return zero, err
	}

	orderItems, err := s.storage.ListOrderItem(ctx, db.ListOrderItemParams{
		OrderID: slice.Map(orders, func(o db.OrderBase) int64 { return o.ID }),
	})
	if err != nil {
		return zero, err
	}
	orderItemsMap := slice.GroupBySlice(orderItems, func(oi db.OrderItem) (int64, db.OrderItem) { return oi.OrderID, oi })

	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data: slice.Map(orders, func(o db.OrderBase) ordermodel.Order {
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
	Account       authmodel.AuthenticatedAccount
	Address       string     `validate:"required"`
	PaymentOption string     `validate:"required,min=1,max=50"`
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
	Order       db.OrderBase `json:"order"`
	RedirectUrl null.String  `json:"url"`
}

func (s *OrderBiz) CreateOrder(ctx context.Context, params CreateOrderParams) (CreateOrderResult, error) {
	var zero CreateOrderResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	// Start transaction
	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	// Remove the checkout items from cart
	skuIDs := slice.Map(params.Skus, func(s OrderSku) int64 { return s.SkuID })
	orderSkuMap := slice.GroupBy(params.Skus, func(s OrderSku) (int64, OrderSku) { return s.SkuID, s })
	cartItems, err := txStorage.RemoveCheckoutItem(ctx, db.RemoveCheckoutItemParams{
		CartID: params.Account.ID,
		SkuID:  skuIDs,
	})
	if err != nil {
		return zero, err
	}
	if len(cartItems) != len(skuIDs) {
		// Prevent duplicate skuIDs in params or some sku not found in cart
		return zero, fmt.Errorf("some sku not found in cart")
	}

	// Reserve stock for the skus in cart
	var reserveStockErr error
	txStorage.ReserveInventory(ctx, slice.Map(cartItems, func(item db.AccountCartItem) db.ReserveInventoryParams {
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
		return zero, reserveStockErr
	}

	// Retrieve skus data
	skus, err := txStorage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return zero, err
	}
	if len(skus) != len(skuIDs) {
		return zero, ordermodel.ErrOrderItemNotFound
	}
	skuMap := slice.GroupBy(skus, func(s db.CatalogProductSku) (int64, db.CatalogProductSku) { return s.ID, s })

	// Calculate prices
	spus, err := txStorage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
		ID: slice.Map(skus, func(s db.CatalogProductSku) int64 { return s.SpuID }),
	})
	if err != nil {
		return zero, err
	}
	spuMap := slice.GroupBy(spus, func(s db.CatalogProductSpu) (int64, db.CatalogProductSpu) { return s.ID, s })

	priceMap, err := s.promotion.CalculatePromotedPrices(ctx, skus, spuMap)
	if err != nil {
		return zero, err
	}
	var totalPrice sharedmodel.Concurrency
	for _, skuID := range skuIDs {
		totalPrice += priceMap[skuID].Price.Mul(orderSkuMap[skuID].Quantity)
	}

	// Create order
	order, err := txStorage.CreateDefaultOrderBase(ctx, db.CreateDefaultOrderBaseParams{
		AccountID:     params.Account.ID,
		PaymentOption: params.PaymentOption,
		Address:       params.Address,
	})
	if err != nil {
		return zero, err
	}

	// Create order items shipments
	var createShipmentArgs []db.CreateBatchOrderShipmentParams

	// get vendor address
	contacts, err := txStorage.GetVendorAddressBySkuIDs(ctx, skuIDs)
	if err != nil {
		return zero, err
	}
	// map[skuID]contact
	contactMap := slice.GroupBy(contacts, func(c db.GetVendorAddressBySkuIDsRow) (int64, db.GetVendorAddressBySkuIDsRow) { return c.SkuID, c })

	for _, orderSku := range params.Skus {
		vendorAddress := contactMap[orderSku.SkuID].Address // TODO: get nearest vendor address instead of default address

		// Only quote shipment, after vendor confirm the order, we will create the shipment
		ship, err := s.shipmentMap[orderSkuMap[orderSku.SkuID].ShipmentOption].Quote(ctx, shipment.CreateParams{
			FromAddress: vendorAddress,
			ToAddress:   params.Address,
			WeightGrams: 10, // TODO: Fetch the real weightgrams, lengthcm, ... in product specification table, dimensions, service, ...
			LengthCM:    10,
			WidthCM:     10,
			HeightCM:    10,
		})
		if err != nil {
			return zero, err
		}

		createShipmentArgs = append(createShipmentArgs, db.CreateBatchOrderShipmentParams{
			FromAddress:  vendorAddress,
			ToAddress:    params.Address,
			Option:       orderSkuMap[orderSku.SkuID].ShipmentOption,
			TrackingCode: pgutil.StringToPgText(""), // To be updated when vendor confirm the order
			LabelUrl:     pgutil.StringToPgText(""), // To be updated when vendor confirm the order
			Status:       db.OrderShipmentStatusPending,
			Cost:         ship.Costs.Int64(),
			DateEta:      pgutil.TimeToPgTimestamptz(ship.ETA),
			WeightGrams:  10, // TODO: Fetch the real weightgrams, lengthcm, ... in product specification table, dimensions, service, ...
			LengthCm:     10,
			WidthCm:      10,
			HeightCm:     10,
			DateCreated:  pgutil.TimeToPgTimestamptz(time.Now()),
		})
	}

	shipmentMap := make(map[int64]db.OrderShipment) // map[skuID]shipment
	var createShipmentErr error
	txStorage.CreateBatchOrderShipment(ctx, createShipmentArgs).QueryRow(func(index int, s db.OrderShipment, err error) {
		createShipmentErr = err
		shipmentMap[skuIDs[index]] = s
	})
	if createShipmentErr != nil {
		return zero, createShipmentErr
	}

	// Create order items
	var createOrderItemArgs []db.CreateBatchOrderItemParams
	for _, skuID := range skuIDs {
		if skuMap[skuID].CanCombine {
			createOrderItemArgs = append(createOrderItemArgs, db.CreateBatchOrderItemParams{
				VendorID:   contactMap[skuID].VendorID,
				OrderID:    order.ID,
				SkuID:      skuID,
				Quantity:   orderSkuMap[skuID].Quantity,
				ShipmentID: shipmentMap[skuID].ID,
				Note:       orderSkuMap[skuID].Note,
				Status:     db.SharedStatusPending,
			})
		} else {
			for i := int64(0); i < orderSkuMap[skuID].Quantity; i++ {
				createOrderItemArgs = append(createOrderItemArgs, db.CreateBatchOrderItemParams{
					VendorID:   contactMap[skuID].VendorID,
					OrderID:    order.ID,
					SkuID:      skuID,
					Quantity:   1,
					ShipmentID: shipmentMap[skuID].ID,
					Note:       orderSkuMap[skuID].Note,
					Status:     db.SharedStatusPending,
				})
			}
		}
	}

	// Get available serial id and attach to order items
	var getProductArgs []db.GetAvailableProductsParams
	for _, skuID := range skuIDs {
		getProductArgs = append(getProductArgs, db.GetAvailableProductsParams{
			SkuID:  skuID,
			Amount: int32(orderSkuMap[skuID].Quantity),
		})
	}

	serialsMap := make(map[int64][]db.GetAvailableProductsRow) // map[skuID][]serial
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

		// rows length should be equal to getProductArgs[i].Amount
		serialsMap[rows[0].SkuID] = rows
	})
	if getSerialsError != nil {
		return zero, getSerialsError
	}

	// Batch create order items and create serials for each item
	var batchErr error
	var serialIDs []int64
	var createOrderSerialArgs []db.CreateCopyDefaultOrderItemSerialParams
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
				// Out of stock
				batchErr = ordermodel.ErrOutOfStock.Fmt(fmt.Sprintf("%s (%d)", spu.Name, orderItem.SkuID))
				return
			}

			// Take the first serial and remove it from the list
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
		return zero, batchErr
	}

	// Attach serials to order items
	if _, err = txStorage.CreateCopyDefaultOrderItemSerial(ctx, createOrderSerialArgs); err != nil {
		return zero, err
	}

	// Update serial status to sold
	if err = txStorage.UpdateSerialStatus(ctx, db.UpdateSerialStatusParams{
		Status: db.InventoryProductStatusSold,
		ID:     serialIDs,
	}); err != nil {
		return zero, err
	}

	// Create order via payment gateway
	var redirectUrl null.String
	if gateway, ok := s.paymentMap[params.PaymentOption]; ok {
		result, err := gateway.CreateOrder(ctx, payment.CreateOrderParams{
			RefID:  order.ID,
			Amount: totalPrice,
			Info:   fmt.Sprintf("ShippingOrder for order %d", order.ID),
		})
		if err != nil {
			return zero, err
		}
		if result.RedirectURL != "" {
			redirectUrl.SetValid(result.RedirectURL)
		}
	} else {
		return zero, ordermodel.ErrPaymentGatewayNotFound
	}

	// TODO: Use outbox pattern to prevent lost event, currently if pubsub fails, rollback the whole transaction
	if err = s.pubsub.Publish(ordermodel.TopicOrderCreated, OrderCreatedParams{
		OrderID: order.ID,
	}); err != nil {
		return zero, err
	}

	if err = txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return CreateOrderResult{Order: order, RedirectUrl: redirectUrl}, nil
}

type CancelOrderParams = struct {
	Account authmodel.AuthenticatedAccount
	OrderID int64
}

func (s *OrderBiz) CancelOrder(ctx context.Context, params CancelOrderParams) error {
	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	payment, err := txStorage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(params.OrderID))
	if err != nil {
		return err
	}

	// No need to check ownership as we already check it in GetOrder
	// if payment.UserID != *params.UserID {
	// 	return fmt.Errorf("payment %d not belong to user %d", params.OrderID, params.UserID)
	// }

	if payment.PaymentStatus != db.SharedStatusPending {
		return fmt.Errorf("payment %d cannot be canceled", params.OrderID)
	}

	if _, err = txStorage.UpdateOrderBase(ctx, db.UpdateOrderBaseParams{
		ID:            params.OrderID,
		PaymentStatus: db.NullSharedStatus{SharedStatus: db.SharedStatusCanceled, Valid: true},
	}); err != nil {
		return err
	}

	if err = txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}

type VerifyPaymentParams struct {
	PaymentGateway string `validate:"required,min=1,max=50"`
	Data           map[string]any
}

func (s *OrderBiz) VerifyPayment(ctx context.Context, params VerifyPaymentParams) error {
	var refID int64

	if err := validator.Validate(params); err != nil {
		return err
	}

	// Verify payment via payment gateway
	if gateway, ok := s.paymentMap[params.PaymentGateway]; ok {
		result, err := gateway.VerifyPayment(ctx, params.Data)
		if err != nil {
			return err
		}
		refID = result.RefID
	} else {
		return ordermodel.ErrPaymentGatewayNotFound
	}

	// Publish event for order paid
	if err := s.pubsub.Publish(ordermodel.TopicOrderPaid, OrderPaidParams{
		OrderID: refID,
	}); err != nil {
		return err
	}

	return nil
}

type QuoteOrderParams struct {
	Account authmodel.AuthenticatedAccount
	Address string     `validate:"required"`
	Skus    []OrderSku `validate:"required,min=1,dive"`
}
type QuoteOrderResult struct {
	Subtotal sharedmodel.Concurrency `json:"subtotal"`
	Shipping sharedmodel.Concurrency `json:"shipping"`
}

func (s *OrderBiz) QuoteOrder(ctx context.Context, params QuoteOrderParams) (QuoteOrderResult, error) {
	var zero QuoteOrderResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	skuIDs := slice.Map(params.Skus, func(s OrderSku) int64 { return s.SkuID })
	skus, err := txStorage.ListCatalogProductSku(ctx, db.ListCatalogProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return zero, err
	}
	skuMap := slice.GroupBy(skus, func(s db.CatalogProductSku) (int64, db.CatalogProductSku) { return s.ID, s })
	orderSkuMap := slice.GroupBy(params.Skus, func(s OrderSku) (int64, OrderSku) { return s.SkuID, s })

	spus, err := txStorage.ListCatalogProductSpu(ctx, db.ListCatalogProductSpuParams{
		ID: slice.Map(skus, func(s db.CatalogProductSku) int64 { return s.SpuID }),
	})
	if err != nil {
		return zero, err
	}
	spuMap := slice.GroupBy(spus, func(s db.CatalogProductSpu) (int64, db.CatalogProductSpu) { return s.ID, s })

	priceMap, err := s.promotion.CalculatePromotedPrices(ctx, skus, spuMap)
	if err != nil {
		return zero, err
	}

	var subtotal, shippingPrice sharedmodel.Concurrency
	for _, sku := range params.Skus {
		subtotal += priceMap[sku.SkuID].Price.Mul(orderSkuMap[sku.SkuID].Quantity)

		vendorContact, err := s.getDefaultContact(ctx, spuMap[skuMap[sku.SkuID].SpuID].AccountID)
		if err != nil {
			return zero, err
		}

		shipment, err := s.shipmentMap[sku.ShipmentOption].Quote(ctx, shipment.CreateParams{
			FromAddress: vendorContact.Address,
			ToAddress:   params.Address,
			WeightGrams: 10, // TODO: change later
			LengthCM:    10,
			WidthCM:     10,
			HeightCM:    10,
		})
		if err != nil {
			return zero, err
		}
		shippingPrice += shipment.Costs.Mul(orderSkuMap[sku.SkuID].Quantity)
	}

	return QuoteOrderResult{
		Subtotal: subtotal,
		Shipping: shippingPrice,
	}, nil
}

func (s *OrderBiz) getDefaultContact(ctx context.Context, accountID int64) (db.AccountContact, error) {
	var zero db.AccountContact

	profile, err := s.storage.GetAccountProfile(ctx, db.GetAccountProfileParams{
		ID: pgutil.Int64ToPgInt8(accountID),
	})
	if err != nil {
		return zero, err
	}

	contact, err := s.storage.GetAccountContact(ctx, profile.DefaultContactID)
	if err != nil {
		return zero, err
	}

	return contact, nil
}
