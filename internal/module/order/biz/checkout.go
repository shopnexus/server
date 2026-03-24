package orderbiz

import (
	"encoding/json"
	"fmt"
	"time"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/payment"
	"shopnexus-server/internal/infras/shipment"
	accountmodel "shopnexus-server/internal/module/account/model"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	analyticdb "shopnexus-server/internal/module/analytic/db/sqlc"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type CheckoutParams struct {
	Account       accountmodel.AuthenticatedAccount
	Address       string         `validate:"required"`
	BuyNow        bool           `validate:"omitempty"`
	PaymentOption string         `validate:"required,min=1,max=100"`
	Items         []CheckoutItem `validate:"required,min=1,dive"`
	// Each items is a order, contains one order item (sku), or more than one if having a promotion bundle
	// TODO: future: add support for wrapping multiple SKUs into one order (only for same vendor)
}

type CheckoutItem struct {
	SkuID          uuid.UUID       `json:"sku_id"`
	Quantity       int64           `json:"quantity"`
	Note           string          `json:"note"`
	ShipmentOption string          `json:"shipment_option"`
	PromotionCodes []string        `json:"promotion_codes"`
	Data           json.RawMessage `json:"data"` // Additional data for this item
}

type CheckoutResult struct {
	Orders      []ordermodel.Order `json:"orders"`
	RedirectUrl null.String        `json:"url"`
}

// Intermediate result types for durable steps
type checkoutProductsResult struct {
	Skus       []catalogmodel.ProductSku `json:"skus"`
	SpusData   []catalogmodel.ProductSpu `json:"spus_data"`
	ContactMap map[uuid.UUID]string      `json:"contact_map"` // accountID → address
}

type checkoutShipmentEntry struct {
	ID   uuid.UUID `json:"id"`
	Cost int64     `json:"cost"`
}

type checkoutPaymentResult struct {
	PaymentID   int64  `json:"payment_id"`
	RedirectURL string `json:"redirect_url"`
}

// Checkout processes a purchase order with payment creation, inventory reservation, and shipment booking.
func (b *OrderBizImpl) Checkout(ctx restate.Context, params CheckoutParams) (CheckoutResult, error) {
	var zero CheckoutResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}
	if params.BuyNow && len(params.Items) != 1 {
		return zero, ordermodel.ErrBuyNowSingleSkuOnly.Terminal()
	}

	skuIDs := lo.Map(params.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	checkoutItemMap := lo.KeyBy(params.Items, func(s CheckoutItem) uuid.UUID { return s.SkuID })

	// Step 1: Fetch product data (catalog + contacts)
	skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{
		ID: skuIDs,
	})
	if err != nil {
		return zero, fmt.Errorf("list catalog product skus: %w", err)
	}
	if len(skus) != len(skuIDs) {
		return zero, ordermodel.ErrOrderItemNotFound.Terminal()
	}

	listSpu, err := b.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{
		Account: params.Account,
		ID:      lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID { return s.SpuID }),
	})
	if err != nil {
		return zero, fmt.Errorf("list catalog product spu: %w", err)
	}

	vendorIDs := lo.Map(listSpu.Data, func(s catalogmodel.ProductSpu, _ int) uuid.UUID { return s.AccountID })
	contactMapRaw, err := b.account.GetDefaultContact(ctx, vendorIDs)
	if err != nil {
		return zero, fmt.Errorf("get default contact map: %w", err)
	}

	// Simplify to just addresses for serialization safety
	contactMap := make(map[uuid.UUID]string, len(contactMapRaw))
	for id, contact := range contactMapRaw {
		contactMap[id] = contact.Address
	}

	products := checkoutProductsResult{
		Skus:       skus,
		SpusData:   listSpu.Data,
		ContactMap: contactMap,
	}

	// Build maps (pure computation)
	skuMap := lo.KeyBy(products.Skus, func(s catalogmodel.ProductSku) uuid.UUID { return s.ID })
	spuMap := lo.KeyBy(products.SpusData, func(s catalogmodel.ProductSpu) uuid.UUID { return s.ID })

	// Step 2: Remove checkout items from cart if not buy now
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
				return ordermodel.ErrSkuNotFoundInCart.Terminal()
			}
			return nil
		}); err != nil {
			return zero, err
		}
	}

	// Step 3: Reserve inventory
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
		return zero, fmt.Errorf("reserve inventory: %w", err)
	}

	serialIDsMap := lo.SliceToMap(inventories, func(i inventorybiz.ReserveInventoryResult) (uuid.UUID, []string) {
		return i.RefID, i.SerialIDs
	})

	// Step 4: Create shipments
	shipmentMap, err := restate.Run(ctx, func(ctx restate.RunContext) (map[uuid.UUID]checkoutShipmentEntry, error) {
		result := make(map[uuid.UUID]checkoutShipmentEntry, len(params.Items))
		for _, checkoutItem := range params.Items {
			shipmentClient, err := b.getShipmentClient(checkoutItem.ShipmentOption)
			if err != nil {
				return nil, fmt.Errorf("get shipment client: %w", err)
			}

			var defaultPackageDetails shipment.PackageDetails
			if err := sonic.Unmarshal(skuMap[checkoutItem.SkuID].PackageDetails, &defaultPackageDetails); err != nil {
				return nil, fmt.Errorf("unmarshal package details for sku %s: %w", checkoutItem.SkuID, err)
			}

			contactAddress := products.ContactMap[spuMap[skuMap[checkoutItem.SkuID].SpuID].AccountID]

			shipmentOrder, err := shipmentClient.Create(ctx, shipment.CreateParams{
				FromAddress: contactAddress,
				ToAddress:   params.Address,
				Package:     defaultPackageDetails,
			})
			if err != nil {
				return nil, fmt.Errorf("create shipment: %w", err)
			}

			dbShipment, err := b.storage.Querier().CreateDefaultShipment(ctx, orderdb.CreateDefaultShipmentParams{
				Option:      checkoutItem.ShipmentOption,
				Cost:        int64(shipmentOrder.Costs),
				DateEta:     shipmentOrder.ETA,
				FromAddress: contactAddress,
				ToAddress:   params.Address,
				WeightGrams: int32(defaultPackageDetails.WeightGrams),
				LengthCm:    int32(defaultPackageDetails.LengthCM),
				WidthCm:     int32(defaultPackageDetails.WidthCM),
				HeightCm:    int32(defaultPackageDetails.HeightCM),
			})
			if err != nil {
				return nil, fmt.Errorf("create shipment: %w", err)
			}

			result[checkoutItem.SkuID] = checkoutShipmentEntry{
				ID:   dbShipment.ID,
				Cost: dbShipment.Cost,
			}
		}
		return result, nil
	})
	if err != nil {
		return zero, err
	}

	// Step 5: Calculate promoted prices
	requestOrderPrices := lo.Map(params.Items, func(item CheckoutItem, _ int) catalogmodel.RequestOrderPrice {
		return catalogmodel.RequestOrderPrice{
			SkuID:          item.SkuID,
			SpuID:          skuMap[item.SkuID].SpuID,
			UnitPrice:      skuMap[item.SkuID].Price,
			Quantity:       item.Quantity,
			ShipCost:       sharedmodel.Concurrency(shipmentMap[item.SkuID].Cost),
			PromotionCodes: item.PromotionCodes,
		}
	})

	priceMap, err := restate.Run(ctx, func(ctx restate.RunContext) (map[uuid.UUID]*catalogmodel.OrderPrice, error) {
		return b.promotion.CalculatePromotedPrices(ctx, promotionbiz.CalculatePromotedPricesParams{Prices: requestOrderPrices, SpuMap: spuMap})
	})
	if err != nil {
		return zero, fmt.Errorf("calculate promoted prices: %w", err)
	}

	// Step 6: Create payment + call payment provider
	paymentInfo, err := restate.Run(ctx, func(ctx restate.RunContext) (checkoutPaymentResult, error) {
		expiryDays := config.GetConfig().App.Order.PaymentExpiryDays
		if expiryDays <= 0 {
			expiryDays = 30
		}

		var totalPrice sharedmodel.Concurrency
		for _, checkoutItem := range params.Items {
			price := priceMap[checkoutItem.SkuID]
			totalPrice += price.Total()
		}

		dbPayment, err := b.storage.Querier().CreateDefaultPayment(ctx, orderdb.CreateDefaultPaymentParams{
			AccountID:   params.Account.ID,
			Option:      params.PaymentOption,
			Amount:      int64(totalPrice),
			Data:        []byte("[]"), // TODO: may put some data here idk
			DateExpired: time.Now().Add(time.Hour * 24 * time.Duration(expiryDays)),
		})
		if err != nil {
			return checkoutPaymentResult{}, fmt.Errorf("create payment: %w", err)
		}

		paymentProvider, err := b.getPaymentClient(params.PaymentOption)
		if err != nil {
			return checkoutPaymentResult{}, fmt.Errorf("get payment provider: %w", err)
		}

		createdOrder, err := paymentProvider.CreateOrder(ctx, payment.CreateOrderParams{
			RefID:  dbPayment.ID,
			Amount: totalPrice,
			Info:   fmt.Sprintf("Order %d", dbPayment.ID),
		})
		if err != nil {
			return checkoutPaymentResult{}, fmt.Errorf("create payment order: %w", err)
		}

		return checkoutPaymentResult{
			PaymentID:   dbPayment.ID,
			RedirectURL: createdOrder.RedirectURL,
		}, nil
	})
	if err != nil {
		return zero, err
	}

	// Step 7: Create orders and order items
	orderIDs, err := restate.Run(ctx, func(ctx restate.RunContext) ([]uuid.UUID, error) {
		var ids []uuid.UUID
		for _, checkoutItem := range params.Items {
			sku := skuMap[checkoutItem.SkuID]
			price := priceMap[checkoutItem.SkuID]
			serialIDs := serialIDsMap[checkoutItem.SkuID]

			order, err := b.storage.Querier().CreateDefaultOrder(ctx, orderdb.CreateDefaultOrderParams{
				CustomerID:      params.Account.ID,
				VendorID:        spuMap[sku.SpuID].AccountID,
				PaymentID:       paymentInfo.PaymentID,
				ShipmentID:      shipmentMap[checkoutItem.SkuID].ID,
				Address:         params.Address,
				ProductCost:     int64(price.ProductCost),
				ProductDiscount: int64(price.Request.UnitPrice.Mul(price.Request.Quantity).Sub(price.ProductCost)),
				ShipCost:        int64(price.ShipCost),
				ShipDiscount:    int64(price.Request.ShipCost.Sub(price.ShipCost)),
				Total:           int64(price.Total()),
				Note:            null.StringFrom(checkoutItem.Note),
				Data:            checkoutItem.Data,
			})
			if err != nil {
				return nil, fmt.Errorf("create order base: %w", err)
			}
			ids = append(ids, order.ID)

			// Create order items
			var createOrderItemArgs []orderdb.CreateCopyItemParams
			if sku.CanCombine {
				jsonSerialIDs, err := sonic.Marshal(serialIDs)
				if err != nil {
					return nil, fmt.Errorf("marshal serial ids: %w", err)
				}

				createOrderItemArgs = append(createOrderItemArgs, orderdb.CreateCopyItemParams{
					OrderID:   order.ID,
					SkuID:     sku.ID,
					SkuName:   spuMap[sku.SpuID].Name,
					Quantity:  checkoutItem.Quantity,
					UnitPrice: int64(price.Request.UnitPrice),
					Note:      null.StringFrom(checkoutItem.Note),
					SerialIds: jsonSerialIDs,
				})
			} else {
				for _, serialID := range serialIDs {
					jsonSerialIDs, err := sonic.Marshal([]string{serialID})
					if err != nil {
						return nil, fmt.Errorf("marshal serial ids: %w", err)
					}

					createOrderItemArgs = append(createOrderItemArgs, orderdb.CreateCopyItemParams{
						OrderID:   order.ID,
						SkuID:     sku.ID,
						SkuName:   spuMap[sku.SpuID].Name,
						Quantity:  1,
						UnitPrice: int64(price.Request.UnitPrice),
						Note:      null.StringFrom(checkoutItem.Note),
						SerialIds: jsonSerialIDs,
					})
				}
			}

			if _, err := b.storage.Querier().CreateCopyItem(ctx, createOrderItemArgs); err != nil {
				return nil, fmt.Errorf("create order items: %w", err)
			}
		}
		return ids, nil
	})
	if err != nil {
		return zero, err
	}

	// Step 8: Fetch created orders (ListOrders has its own Run internally)
	orders, err := b.ListOrders(ctx, ListOrdersParams{
		ID: orderIDs,
	})
	if err != nil {
		return zero, fmt.Errorf("fetch created orders: %w", err)
	}

	// Step 9: Track purchase interactions
	var purchaseInteractions []analyticbiz.CreateInteraction
	for _, item := range params.Items {
		purchaseInteractions = append(purchaseInteractions, analyticbiz.CreateInteraction{
			Account:   params.Account,
			EventType: analyticmodel.EventPurchase,
			RefType:   analyticdb.AnalyticInteractionRefTypeProduct,
			RefID:     item.SkuID.String(),
		})
	}
	restate.ServiceSend(ctx, "AnalyticBiz", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
		Interactions: purchaseInteractions,
	})

	return CheckoutResult{
		Orders:      orders.Data,
		RedirectUrl: null.StringFrom(paymentInfo.RedirectURL),
	}, nil
}

type CancelOrderParams = struct {
	Account accountmodel.AuthenticatedAccount
	OrderID uuid.UUID
}

// CancelOrder cancels a pending order along with its payment and shipment.
func (b *OrderBizImpl) CancelOrder(ctx restate.Context, params CancelOrderParams) error {
	// GetOrder has its own Run internally
	order, err := b.GetOrder(ctx, params.OrderID)
	if err != nil {
		return fmt.Errorf("fetch order: %w", err)
	}

	// Fetch shipment status
	shipmentStatus, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderShipmentStatus, error) {
		s, err := b.storage.Querier().GetShipment(ctx, uuid.NullUUID{UUID: order.ShipmentID, Valid: true})
		if err != nil {
			return "", fmt.Errorf("fetch shipment: %w", err)
		}
		return s.Status, nil
	})
	if err != nil {
		return err
	}

	// Validate cancellation (pure checks)
	if order.Payment.Status != orderdb.OrderStatusPending {
		return ordermodel.ErrPaymentCannotCancel.Terminal()
	}
	if shipmentStatus != orderdb.OrderShipmentStatusPending {
		return ordermodel.ErrShipmentCannotCancel.Terminal()
	}
	if order.Status != orderdb.OrderStatusPending {
		return ordermodel.ErrOrderCannotCancel.Terminal()
	}

	// Cancel payment, shipment, and order
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		if _, err := b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
			ID:     order.Payment.ID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("update payment status: %w", err)
		}

		if _, err := b.storage.Querier().UpdateShipment(ctx, orderdb.UpdateShipmentParams{
			ID:     order.ShipmentID,
			Status: orderdb.NullOrderShipmentStatus{OrderShipmentStatus: orderdb.OrderShipmentStatusCancelled, Valid: true},
		}); err != nil {
			return fmt.Errorf("update shipment status: %w", err)
		}

		if _, err := b.storage.Querier().UpdateOrder(ctx, orderdb.UpdateOrderParams{
			ID:     order.ID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("update order status: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

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
	restate.ServiceSend(ctx, "AnalyticBiz", "CreateInteraction").Send(analyticbiz.CreateInteractionParams{
		Interactions: cancelInteractions,
	})

	return nil
}
