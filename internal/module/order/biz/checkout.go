package orderbiz

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/shipment"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	catalogbiz "shopnexus-remastered/internal/module/catalog/biz"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	inventorybiz "shopnexus-remastered/internal/module/inventory/biz"
	inventorydb "shopnexus-remastered/internal/module/inventory/db/sqlc"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	sharedmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type CheckoutParams struct {
	Storage       OrderStorage
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

func (b *OrderBiz) Checkout(ctx context.Context, params CheckoutParams) (CheckoutResult, error) {
	var zero CheckoutResult

	if err := validator.Validate(params); err != nil {
		return zero, err
	}
	if params.BuyNow && len(params.Items) != 1 {
		return zero, fmt.Errorf("buy now only support single sku")
	}

	skuIDs := lo.Map(params.Items, func(s CheckoutItem, _ int) uuid.UUID { return s.SkuID })
	checkoutItemMap := lo.KeyBy(params.Items, func(s CheckoutItem) uuid.UUID { return s.SkuID })
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

	listSpu, err := b.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{
		ID: lo.Map(skus, func(s catalogmodel.ProductSku, _ int) uuid.UUID { return s.SpuID }),
	})
	if err != nil {
		return zero, fmt.Errorf("failed to list catalog product spu: %w", err)
	}
	spuMap := lo.KeyBy(listSpu.Data, func(s catalogmodel.ProductSpu) uuid.UUID { return s.ID })

	vendorIDs := lo.Map(listSpu.Data, func(s catalogmodel.ProductSpu, _ int) uuid.UUID { return s.AccountID })
	contactMap, err := b.account.GetDefaultContact(ctx, vendorIDs)
	if err != nil {
		return zero, fmt.Errorf("failed to get default contact map: %w", err)
	}

	var (
		redirectUrl null.String
		orderIDs    []uuid.UUID
	)

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage OrderStorage) error {
		// First step: remove checkout items from cart if not buy now
		if !params.BuyNow {
			cartItems, err := txStorage.Querier().RemoveCheckoutItem(ctx, orderdb.RemoveCheckoutItemParams{
				AccountID: params.Account.ID,
				SkuID:     skuIDs,
			})
			if err != nil {
				return fmt.Errorf("failed to remove checkout items: %w", err)
			}
			if len(cartItems) != len(skuIDs) {
				return fmt.Errorf("some sku not found in cart")
			}
		}

		// Next step: reserve inventory
		// TODO: should use message queue in order to atomically reserve inventory
		inventories, err := b.inventory.ReserveInventory(ctx, inventorybiz.ReserveInventoryParams{
			Items: lo.Map(params.Items, func(item CheckoutItem, _ int) inventorybiz.ReserveIventory {
				return inventorybiz.ReserveIventory{
					RefType: inventorydb.InventoryStockRefTypeProductSku,
					RefID:   item.SkuID,
					Amount:  checkoutItemMap[item.SkuID].Quantity,
				}
			}),
		})
		if err != nil {
			return fmt.Errorf("failed to reserve inventory: %w", err)
		}
		// Because we only use ref type product sku then we can map by RefID here
		// map[skuID][]string
		serialIDsMap := lo.SliceToMap(inventories, func(i inventorybiz.ReserveInventoryResult) (uuid.UUID, []string) { return i.RefID, i.SerialIDs })

		// Next step: create shipments (each checkout item have a shipment)
		var shipmentMap map[uuid.UUID]orderdb.OrderShipment
		for _, checkoutItem := range params.Items {
			shipmentClient, err := b.getShipmentClient(checkoutItem.ShipmentOption)
			if err != nil {
				return fmt.Errorf("failed to get shipment client: %w", err)
			}

			var defaultPackageDetails shipment.PackageDetails
			if err := sonic.Unmarshal(skuMap[checkoutItem.SkuID].PackageDetails, &defaultPackageDetails); err != nil {
				return fmt.Errorf("failed to unmarshal package details for sku %s: %w", checkoutItem.SkuID, err)
			}

			contact := contactMap[spuMap[skuMap[checkoutItem.SkuID].SpuID].AccountID]

			shipmentOrder, err := shipmentClient.Create(ctx, shipment.CreateParams{
				FromAddress: contact.Address,
				ToAddress:   params.Address,
				Package:     defaultPackageDetails,
			})
			if err != nil {
				return fmt.Errorf("failed to create shipment: %w", err)
			}

			dbShipment, err := txStorage.Querier().CreateDefaultShipment(ctx, orderdb.CreateDefaultShipmentParams{
				Option:      checkoutItem.ShipmentOption,
				Cost:        int64(shipmentOrder.Costs),
				DateEta:     shipmentOrder.ETA,
				FromAddress: contact.Address,
				ToAddress:   params.Address,
				WeightGrams: int32(defaultPackageDetails.WeightGrams),
				LengthCm:    int32(defaultPackageDetails.LengthCM),
				WidthCm:     int32(defaultPackageDetails.WidthCM),
				HeightCm:    int32(defaultPackageDetails.HeightCM),
			})
			if err != nil {
				return fmt.Errorf("failed to create shipment: %w", err)
			}

			shipmentMap[checkoutItem.SkuID] = dbShipment
		}

		// Next step: calculate promoted prices
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
		priceMap, err := b.promotion.CalculatePromotedPrices(ctx, requestOrderPrices, spuMap)
		if err != nil {
			return fmt.Errorf("failed to calculate promoted prices: %w", err)
		}

		// Next step: create orders (each checkout item is a order)
		for _, checkoutItem := range params.Items {
			sku := skuMap[checkoutItem.SkuID]
			price := priceMap[checkoutItem.SkuID]
			serialIDs := serialIDsMap[checkoutItem.SkuID]

			// Create payment
			expiryDays := config.GetConfig().App.Order.PaymentExpiryDays
			if expiryDays <= 0 {
				expiryDays = 30
			}
			payment, err := txStorage.Querier().CreateDefaultPayment(ctx, orderdb.CreateDefaultPaymentParams{
				AccountID:   params.Account.ID,
				Option:      params.PaymentOption,
				Amount:      int64(price.Total()),
				Data:        checkoutItem.Data,
				DateExpired: time.Now().Add(time.Hour * 24 * time.Duration(expiryDays)),
			})
			if err != nil {
				return fmt.Errorf("failed to create payment: %w", err)
			}

			// Create order
			order, err := txStorage.Querier().CreateDefaultOrder(ctx, orderdb.CreateDefaultOrderParams{
				CustomerID:      params.Account.ID,
				VendorID:        spuMap[sku.SpuID].AccountID,
				PaymentID:       payment.ID,
				ShipmentID:      shipmentMap[checkoutItem.SkuID].ID,
				Address:         params.Address,
				ProductCost:     int64(price.ProductCost),
				ProductDiscount: int64(price.Request.UnitPrice.Mul(price.Request.Quantity).Sub(price.ProductCost)),
				ShipCost:        int64(price.ShipCost),
				ShipDiscount:    int64(price.ShipCost.Sub(price.ShipCost)),
				Total:           int64(price.Total()),
				Note:            null.StringFrom(checkoutItem.Note),
				Data:            checkoutItem.Data,
			})
			if err != nil {
				return fmt.Errorf("failed to create order base: %w", err)
			}
			orderIDs = append(orderIDs, order.ID)

			// Create order items
			var createOrderItemArgs []orderdb.CreateCopyItemParams
			// If can combine the serial ids into one order item, then create one order item, otherwise create one order item for each serial id
			if sku.CanCombine {
				jsonSerialIDs, err := sonic.Marshal(serialIDs)
				if err != nil {
					return fmt.Errorf("failed to marshal serial ids: %w", err)
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
						return fmt.Errorf("failed to marshal serial ids: %w", err)
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

			// Create order items
			if _, err := txStorage.Querier().CreateCopyItem(ctx, createOrderItemArgs); err != nil {
				return fmt.Errorf("failed to create order items: %w", err)
			}
		}

		return nil
	}); err != nil {
		return zero, fmt.Errorf("failed to create order: %w", err)
	}

	// Fetch created orders
	orders, err := b.ListOrders(ctx, ListOrdersParams{
		ID: orderIDs,
	})
	if err != nil {
		return zero, fmt.Errorf("failed to fetch created orders: %w", err)
	}

	return CheckoutResult{Orders: orders.Data, RedirectUrl: redirectUrl}, nil
}

type CancelOrderParams = struct {
	Storage OrderStorage
	Account accountmodel.AuthenticatedAccount
	OrderID uuid.UUID
}

func (b *OrderBiz) CancelOrder(ctx context.Context, params CancelOrderParams) error {
	order, err := b.GetOrder(ctx, params.OrderID)
	if err != nil {
		return fmt.Errorf("failed to fetch order: %w", err)
	}

	shipment, err := b.storage.Querier().GetShipment(ctx, uuid.NullUUID{UUID: order.ShipmentID, Valid: true})
	if err != nil {
		return fmt.Errorf("failed to fetch shipment: %w", err)
	}

	// Check if the payment is pending
	if order.Payment.Status != orderdb.OrderStatusPending {
		return fmt.Errorf("payment %d cannot be canceled", order.Payment.ID)
	}
	// Check if the shipment is pending
	if shipment.Status != orderdb.OrderShipmentStatusPending {
		return fmt.Errorf("shipment %s cannot be canceled", shipment.ID)
	}
	// Check if the order is pending
	if order.Status != orderdb.OrderStatusPending {
		return fmt.Errorf("order %s cannot be canceled", order.ID)
	}

	// Cancel the order
	// 1. Cancel the payment
	// 2. Cancel the shipment
	// 3. Cancel the order
	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage OrderStorage) error {
		if _, err := txStorage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
			ID:     order.Payment.ID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("failed to update payment status: %w", err)
		}

		if _, err := txStorage.Querier().UpdateShipment(ctx, orderdb.UpdateShipmentParams{
			ID:     order.ShipmentID,
			Status: orderdb.NullOrderShipmentStatus{OrderShipmentStatus: orderdb.OrderShipmentStatusCancelled, Valid: true},
		}); err != nil {
			return fmt.Errorf("failed to update shipment status: %w", err)
		}

		if _, err := txStorage.Querier().UpdateOrder(ctx, orderdb.UpdateOrderParams{
			ID:     order.ID,
			Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusCanceled, Valid: true},
		}); err != nil {
			return fmt.Errorf("failed to update order status: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to cancel order %s: %w", params.OrderID, err)
	}

	return nil
}
