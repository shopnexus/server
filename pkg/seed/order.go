package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"shopnexus-remastered/internal/utils/pgutil"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/utils/ptr"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jaswdr/faker/v2"
)

// PaymentSeedData holds seeded payment data for other seeders to reference
type PaymentSeedData struct {
	PaymentGateways  []db.OrderPaymentGateway
	Orders           []db.OrderBase
	OrderItems       []db.OrderItem
	OrderItemSerials []db.OrderItemSerial
	Shipments        []db.OrderShipment
	Refunds          []db.OrderRefund
	RefundDisputes   []db.OrderRefundDispute
	Invoices         []db.OrderInvoice
}

// SeedPaymentSchema seeds the payment schema with fake data
func SeedPaymentSchema(ctx context.Context, storage db.Querier, fake *faker.Faker, cfg *SeedConfig, accountData *AccountSeedData, catalogData *CatalogSeedData, inventoryData *InventorySeedData) (*PaymentSeedData, error) {
	fmt.Println("💳 Seeding payment schema...")

	// Tạo unique tracker để theo dõi tính duy nhất
	tracker := NewUniqueTracker()

	data := &PaymentSeedData{
		PaymentGateways:  make([]db.OrderPaymentGateway, 0),
		Orders:           make([]db.OrderBase, 0),
		OrderItems:       make([]db.OrderItem, 0),
		OrderItemSerials: make([]db.OrderItemSerial, 0),
		Shipments:        make([]db.OrderShipment, 0),
		Refunds:          make([]db.OrderRefund, 0),
		RefundDisputes:   make([]db.OrderRefundDispute, 0),
		Invoices:         make([]db.OrderInvoice, 0),
	}

	if len(accountData.Customers) == 0 || len(catalogData.ProductSkus) == 0 {
		fmt.Println("⚠️ No customers or product SKUs found, skipping payment seeding")
		return data, nil
	}

	// First, create payment gateways
	fmt.Println("🏦 Creating payment gateways...")
	paymentGatewayParams := []db.CreateCopyOrderPaymentGatewayParams{
		{
			ID:          "cod",
			Method:      db.OrderPaymentMethodCOD,
			Description: pgtype.Text{String: "Pay when you receive the product", Valid: true},
			IsActive:    true,
		},
		{
			ID:          "vnpay_card",
			Method:      db.OrderPaymentMethodCard,
			Description: pgtype.Text{String: "Vietnam's leading payment gateway", Valid: true},
			IsActive:    true,
		},
		{
			ID:          "vnpay_banktransfer",
			Method:      db.OrderPaymentMethodBankTransfer,
			Description: pgtype.Text{String: "Vietnam's leading payment gateway", Valid: true},
			IsActive:    true,
		},
		{
			ID:          "zalopay",
			Method:      db.OrderPaymentMethodBankTransfer,
			Description: pgtype.Text{String: "Digital wallet by Zalo", Valid: true},
			IsActive:    true,
		},
		{
			ID:          "momo",
			Method:      db.OrderPaymentMethodBankTransfer,
			Description: pgtype.Text{String: "Mobile money wallet", Valid: true},
			IsActive:    true,
		},
		{
			ID:          "binance",
			Method:      db.OrderPaymentMethodCrypto,
			Description: pgtype.Text{String: "Cryptocurrency exchange", Valid: true},
			IsActive:    true,
		},
		{
			ID:          "bybit",
			Method:      db.OrderPaymentMethodCrypto,
			Description: pgtype.Text{String: "Cryptocurrency trading platform", Valid: true},
			IsActive:    true,
		},
	}

	// Bulk insert payment gateways
	_, err := storage.CreateCopyOrderPaymentGateway(ctx, paymentGatewayParams)
	if err != nil {
		return nil, fmt.Errorf("failed to bulk create payment gateways: %w", err)
	}

	// Query back created payment gateways
	paymentGateways, err := storage.ListOrderPaymentGateway(ctx, db.ListOrderPaymentGatewayParams{
		Limit:  pgutil.Int32ToPgInt4(int32(len(paymentGatewayParams) * 2)),
		Offset: pgutil.Int32ToPgInt4(0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query back created payment gateways: %w", err)
	}

	data.PaymentGateways = paymentGateways

	// Create shipments
	fmt.Println("📦 Creating shipments...")
	shipmentProviders := []string{"GHN", "GHTK", "DHL", "Viettel Post", "J&T Express"}
	shipmentStatuses := db.AllOrderShipmentStatusValues()

	// Create shipments for orders (one shipment per order)
	shipmentParams := make([]db.CreateCopyOrderShipmentParams, len(data.Orders))
	for i, order := range data.Orders {
		provider := shipmentProviders[fake.RandomDigit()%len(shipmentProviders)]
		status := shipmentStatuses[fake.RandomDigit()%len(shipmentStatuses)]

		var trackingCode *string
		var labelUrl *string
		var estimatedEtd *time.Time
		var dateShipped *time.Time
		var dateDelivered *time.Time

		// Generate realistic data based on status
		if status != "Pending" {
			trackingCodeStr := fmt.Sprintf("%s%s%d", provider[:3], fake.RandomDigit()%10000, fake.RandomDigit()%1000)
			trackingCode = &trackingCodeStr
		}

		if status == "LabelCreated" || status == "InTransit" || status == "OutForDelivery" || status == "Delivered" {
			labelUrlStr := fmt.Sprintf("https://labels.example.com/%d.pdf", order.ID)
			labelUrl = &labelUrlStr
		}

		if status == "InTransit" || status == "OutForDelivery" || status == "Delivered" {
			etd := time.Now().AddDate(0, 0, fake.RandomDigit()%7+1)
			estimatedEtd = &etd
		}

		if status == "InTransit" || status == "OutForDelivery" || status == "Delivered" {
			shipped := time.Now().AddDate(0, 0, -fake.RandomDigit()%3)
			dateShipped = &shipped
		}

		if status == "Delivered" {
			delivered := time.Now().AddDate(0, 0, -fake.RandomDigit()%2)
			dateDelivered = &delivered
		}

		shipmentParams[i] = db.CreateCopyOrderShipmentParams{
			Provider:      provider,
			TrackingCode:  pgtype.Text{String: ptr.DerefDefault(trackingCode, ""), Valid: trackingCode != nil},
			Status:        status,
			LabelUrl:      pgtype.Text{String: ptr.DerefDefault(labelUrl, ""), Valid: labelUrl != nil},
			Cost:          int64(fake.RandomDigit()%50000 + 10000), // 10k-60k VND
			EstimatedEtd:  pgtype.Timestamptz{Time: ptr.DerefDefault(estimatedEtd, time.Time{}), Valid: estimatedEtd != nil},
			DateShipped:   pgtype.Timestamptz{Time: ptr.DerefDefault(dateShipped, time.Time{}), Valid: dateShipped != nil},
			DateDelivered: pgtype.Timestamptz{Time: ptr.DerefDefault(dateDelivered, time.Time{}), Valid: dateDelivered != nil},
		}
	}

	// Bulk insert shipments
	_, err = storage.CreateCopyOrderShipment(ctx, shipmentParams)
	if err != nil {
		return nil, fmt.Errorf("failed to bulk create shipments: %w", err)
	}

	// Query back created shipments
	shipments, err := storage.ListOrderShipment(ctx, db.ListOrderShipmentParams{
		Limit:  pgutil.Int32ToPgInt4(int32(len(shipmentParams) * 2)),
		Offset: pgutil.Int32ToPgInt4(0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query back created shipments: %w", err)
	}

	data.Shipments = shipments

	// Create a mapping from payment method to available gateways
	paymentMethodToGateways := make(map[string][]string)
	for _, gateway := range data.PaymentGateways {
		paymentMethodToGateways[string(gateway.Method)] = append(paymentMethodToGateways[string(gateway.Method)], gateway.ID)
	}

	paymentMethods := db.AllOrderPaymentMethodValues()
	statuses := db.AllSharedStatusValues()

	// Prepare bulk order data
	orderParams := make([]db.CreateCopyOrderBaseParams, cfg.OrderCount)
	for i := 0; i < cfg.OrderCount; i++ {
		customer := accountData.Customers[fake.RandomDigit()%len(accountData.Customers)]
		customerAddress := ""

		// FindAccount an address for this customer
		for _, addr := range accountData.Addresses {
			if addr.AccountID == customer.ID {
				customerAddress = fmt.Sprintf("%s, %s, %s, %s",
					addr.AddressLine, addr.City, addr.StateProvince, addr.Country)
				break
			}
		}

		if customerAddress == "" {
			customerAddress = fake.Address().Address()
		}

		paymentMethod := paymentMethods[fake.RandomDigit()%len(paymentMethods)]

		// Randomly select a gateway from available options for this payment method
		availableGateways := paymentMethodToGateways[string(paymentMethod)]
		var paymentGatewayID string
		if len(availableGateways) > 0 {
			paymentGatewayID = availableGateways[fake.RandomDigit()%len(availableGateways)]
		} else {
			// Fallback to first gateway if no specific mapping found
			paymentGatewayID = data.PaymentGateways[0].ID
		}

		orderParams[i] = db.CreateCopyOrderBaseParams{
			AccountID:      customer.ID,
			PaymentGateway: paymentGatewayID,
			Status:         statuses[fake.RandomDigit()%len(statuses)],
			Address:        customerAddress,
			DateCreated:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
			DateUpdated:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}
	}

	// Bulk insert orders
	_, err = storage.CreateCopyOrderBase(ctx, orderParams)
	if err != nil {
		return nil, fmt.Errorf("failed to bulk create orders: %w", err)
	}

	// Query back created orders
	orders, err := storage.ListOrderBase(ctx, db.ListOrderBaseParams{
		Limit:  pgutil.Int32ToPgInt4(int32(len(orderParams) * 2)),
		Offset: pgutil.Int32ToPgInt4(0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query back created orders: %w", err)
	}

	// Không còn dùng code, lấy toàn bộ orders
	data.Orders = orders

	// Prepare bulk order items and related data
	var orderItemParams []db.CreateCopyOrderItemParams
	var orderItemSerialParams []db.CreateCopyOrderItemSerialParams
	var refundParams []db.CreateCopyOrderRefundParams
	var refundDisputeParams []db.CreateCopyOrderRefundDisputeParams
	var invoiceParams []db.CreateCopyOrderInvoiceParams

	// Create mapping from order ID to shipment ID
	orderToShipment := make(map[int64]int64)
	for i, order := range data.Orders {
		if i < len(data.Shipments) {
			orderToShipment[order.ID] = data.Shipments[i].ID
		}
	}

	orderTotals := make(map[int64]int64) // order ID -> total

	// CreateAccount order items for each order
	for _, order := range data.Orders {
		itemCount := fake.RandomDigit()%5 + 1
		orderTotal := int64(0)

		var currentOrderItems []db.CreateCopyOrderItemParams
		for j := 0; j < itemCount; j++ {
			sku := catalogData.ProductSkus[fake.RandomDigit()%len(catalogData.ProductSkus)]
			quantity := int64(fake.RandomDigit()%3 + 1) // 1-3 items

			orderItemParam := db.CreateCopyOrderItemParams{
				OrderID:    order.ID,
				SkuID:      sku.ID,
				ShipmentID: pgtype.Int8{Int64: orderToShipment[order.ID], Valid: orderToShipment[order.ID] != 0},
				Quantity:   quantity,
			}
			orderItemParams = append(orderItemParams, orderItemParam)
			currentOrderItems = append(currentOrderItems, orderItemParam)
			orderTotal += sku.Price * quantity

			// Store serial assignments for later (we'll need actual order item IDs)
			var availableSerials []db.InventorySkuSerial
			for _, serial := range inventoryData.ProductSerials {
				if serial.SkuID == sku.ID && serial.Status == "Active" {
					availableSerials = append(availableSerials, serial)
				}
			}

			if len(availableSerials) > 0 {
				serialsToAssign := int(quantity)
				if serialsToAssign > len(availableSerials) {
					serialsToAssign = len(availableSerials)
				}

				for k := 0; k < serialsToAssign; k++ {
					serial := availableSerials[k]
					// Store with item code as temporary reference
					orderItemSerialParams = append(orderItemSerialParams, db.CreateCopyOrderItemSerialParams{
						OrderItemID:     0, // Will be filled after order item creation
						ProductSerialID: serial.ID,
					})
				}
			}
		}
		_ = currentOrderItems
		orderTotals[order.ID] = orderTotal

		// Prepare refunds for some order items (10% chance)
		for _, orderItem := range data.OrderItems {
			if fake.RandomDigit()%10 == 0 && order.Status == "Success" {
				refundMethods := db.AllOrderRefundMethodValues()
				refundStatuses := db.AllSharedStatusValues()

				var reviewerID *int64
				if len(accountData.Vendors) > 0 && fake.Boolean().Bool() {
					vendor := accountData.Vendors[fake.RandomDigit()%len(accountData.Vendors)]
					reviewerID = &vendor.ID
				}

				refundAddress := ""
				if fake.Boolean().Bool() { // 50% chance of having pickup address
					refundAddress = fake.Address().Address()
				}

				refundParams = append(refundParams, db.CreateCopyOrderRefundParams{
					OrderItemID:  orderItem.ID,
					ReviewedByID: pgtype.Int8{Int64: ptr.DerefDefault(reviewerID, 0), Valid: reviewerID != nil},
					ShipmentID:   pgtype.Int8{Int64: orderToShipment[orderItem.OrderID], Valid: orderToShipment[orderItem.OrderID] != 0},
					Method:       refundMethods[fake.RandomDigit()%len(refundMethods)],
					Status:       refundStatuses[fake.RandomDigit()%len(refundStatuses)],
					Reason:       generateRefundReason(fake),
					Address:      pgtype.Text{String: refundAddress, Valid: true},
					DateCreated:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
				})
			}
		}

	}

	// Bulk insert order items
	if len(orderItemParams) > 0 {
		_, err = storage.CreateCopyOrderItem(ctx, orderItemParams)
		if err != nil {
			return nil, fmt.Errorf("failed to bulk create order items: %w", err)
		}

		// Query back created order items
		orderItems, err := storage.ListOrderItem(ctx, db.ListOrderItemParams{
			Limit:  pgutil.Int32ToPgInt4(int32(len(orderItemParams) * 2)),
			Offset: pgutil.Int32ToPgInt4(0),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query back created order items: %w", err)
		}

		// No code field; just use fetched records
		data.OrderItems = orderItems

		// Update order item serial parameters with actual order item IDs
		// This is a simplified approach - we'll just create serials for the first few items
		serialIndex := 0
		for _, orderItem := range data.OrderItems {
			// FindAccount available serials for this SKU
			var availableSerials []db.InventorySkuSerial
			for _, serial := range inventoryData.ProductSerials {
				if serial.SkuID == orderItem.SkuID && serial.Status == "Active" {
					availableSerials = append(availableSerials, serial)
				}
			}

			if len(availableSerials) > 0 && serialIndex < len(orderItemSerialParams) {
				serialsToAssign := int(orderItem.Quantity)
				if serialsToAssign > len(availableSerials) {
					serialsToAssign = len(availableSerials)
				}

				for k := 0; k < serialsToAssign && serialIndex < len(orderItemSerialParams); k++ {
					orderItemSerialParams[serialIndex].OrderItemID = orderItem.ID
					serialIndex++
				}
			}
		}
	}

	// Bulk insert order item serials
	if len(orderItemSerialParams) > 0 {
		// Filter out serials without valid order item IDs
		validSerialParams := make([]db.CreateCopyOrderItemSerialParams, 0)
		for _, serial := range orderItemSerialParams {
			if serial.OrderItemID > 0 {
				validSerialParams = append(validSerialParams, serial)
			}
		}

		if len(validSerialParams) > 0 {
			_, err = storage.CreateCopyOrderItemSerial(ctx, validSerialParams)
			if err != nil {
				return nil, fmt.Errorf("failed to bulk create order item serials: %w", err)
			}

			// Query back created order item serials
			orderItemSerials, err := storage.ListOrderItemSerial(ctx, db.ListOrderItemSerialParams{
				Limit:  pgutil.Int32ToPgInt4(int32(len(validSerialParams) * 2)),
				Offset: pgutil.Int32ToPgInt4(0),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to query back created order item serials: %w", err)
			}

			// Populate data.OrderItemSerials with actual database records
			data.OrderItemSerials = orderItemSerials
		}
	}

	// Bulk insert refunds
	if len(refundParams) > 0 {
		_, err = storage.CreateCopyOrderRefund(ctx, refundParams)
		if err != nil {
			return nil, fmt.Errorf("failed to bulk create refunds: %w", err)
		}

		// Query back created refunds
		refunds, err := storage.ListOrderRefund(ctx, db.ListOrderRefundParams{
			Limit:  pgutil.Int32ToPgInt4(int32(len(refundParams) * 2)),
			Offset: pgutil.Int32ToPgInt4(0),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query back created refunds: %w", err)
		}

		// Populate data.Refunds and prepare disputes
		data.Refunds = refunds
		for _, refund := range data.Refunds {
			if fake.RandomDigit()%5 == 0 && len(accountData.Vendors) > 0 {
				vendor := accountData.Vendors[fake.RandomDigit()%len(accountData.Vendors)]
				refundDisputeParams = append(refundDisputeParams, db.CreateCopyOrderRefundDisputeParams{
					RefundID:    refund.ID,
					IssuedByID:  vendor.ID,
					Reason:      generateDisputeReason(fake),
					Status:      "Pending",
					DateCreated: pgtype.Timestamptz{Time: time.Now(), Valid: true},
					DateUpdated: pgtype.Timestamptz{Time: time.Now(), Valid: true},
				})
			}
		}
	}

	// Bulk insert refund disputes
	if len(refundDisputeParams) > 0 {
		_, err = storage.CreateCopyOrderRefundDispute(ctx, refundDisputeParams)
		if err != nil {
			return nil, fmt.Errorf("failed to bulk create refund disputes: %w", err)
		}

		// Query back created refund disputes
		refundDisputes, err := storage.ListOrderRefundDispute(ctx, db.ListOrderRefundDisputeParams{
			Limit:  pgutil.Int32ToPgInt4(int32(len(refundDisputeParams) * 2)),
			Offset: pgutil.Int32ToPgInt4(0),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query back created refund disputes: %w", err)
		}

		// Populate data.RefundDisputes with actual database records
		data.RefundDisputes = refundDisputes
	}

	// Create invoices for completed orders after order items are created
	for _, order := range data.Orders {
		if order.Status == "Success" {
			invoiceTypes := db.AllOrderInvoiceTypeValues()
			customer := getCustomerByID(accountData.Customers, order.AccountID)
			account := getAccountByID(accountData.Accounts, order.AccountID)
			profile := getProfileByID(accountData.Profiles, order.AccountID)

			// Generate hash for this invoice
			hash := []byte(fake.Hash().SHA256())
			var prevHash []byte
			if len(invoiceParams) > 0 {
				prevHash = invoiceParams[len(invoiceParams)-1].Hash // Use previous invoice's hash
			} else {
				prevHash = []byte("genesis") // First invoice
			}

			// Create comprehensive invoice data
			invoiceData := map[string]interface{}{
				"customer": map[string]interface{}{
					"id":    customer.ID,
					"name":  profile.Name,
					"email": account.Email,
					"phone": generateUniquePhoneWithTracker(fake, tracker),
				},
				"order": map[string]interface{}{
					"id":              order.ID,
					"payment_gateway": order.PaymentGateway,
					"address":         order.Address,
					"date_created":    order.DateCreated.Time,
				},
				"items": []map[string]interface{}{},
				"totals": map[string]interface{}{
					"subtotal": orderTotals[order.ID],
					"total":    orderTotals[order.ID] - int64(fake.RandomDigit()%100), // Small discount
				},
			}

			// Add order items to invoice data
			for _, orderItem := range data.OrderItems {
				if orderItem.OrderID == order.ID {
					sku := getSKUByID(catalogData.ProductSkus, orderItem.SkuID)
					if sku != nil {
						spu := getSPUByID(catalogData.ProductSpus, sku.SpuID)
						itemData := map[string]interface{}{
							"sku_id":     sku.ID,
							"quantity":   orderItem.Quantity,
							"unit_price": sku.Price,
							"subtotal":   sku.Price * orderItem.Quantity,
						}
						if spu != nil {
							itemData["product_name"] = spu.Name
						}
						invoiceData["items"] = append(invoiceData["items"].([]map[string]interface{}), itemData)
					}
				}
			}

			// Marshal invoice data to JSON
			invoiceDataJSON, err := json.Marshal(invoiceData)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal invoice data: %w", err)
			}

			invoiceParams = append(invoiceParams, db.CreateCopyOrderInvoiceParams{
				RefType:     "Order",
				RefID:       order.ID,
				Type:        invoiceTypes[fake.RandomDigit()%len(invoiceTypes)],
				ReceiverID:  customer.ID,
				Note:        pgtype.Text{String: fmt.Sprintf("Invoice for order #%d", order.ID), Valid: true},
				Data:        invoiceDataJSON,
				FileRsID:    fake.UUID().V4(),
				Hash:        hash,
				PrevHash:    prevHash,
				DateCreated: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			})
		}
	}

	// Bulk insert invoices
	if len(invoiceParams) > 0 {
		_, err = storage.CreateCopyOrderInvoice(ctx, invoiceParams)
		if err != nil {
			return nil, fmt.Errorf("failed to bulk create invoices: %w", err)
		}

		// Query back created invoices
		invoices, err := storage.ListOrderInvoice(ctx, db.ListOrderInvoiceParams{
			Limit:  pgutil.Int32ToPgInt4(int32(len(invoiceParams) * 2)),
			Offset: pgutil.Int32ToPgInt4(0),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to query back created invoices: %w", err)
		}

		// Populate data.Invoices with actual database records
		data.Invoices = invoices
	}

	fmt.Printf("✅ Payment schema seeded: %d payment gateways, %d orders, %d order items, %d order serials, %d shipments, %d refunds, %d disputes, %d invoices\n",
		len(data.PaymentGateways), len(data.Orders), len(data.OrderItems), len(data.OrderItemSerials), len(data.Shipments),
		len(data.Refunds), len(data.RefundDisputes), len(data.Invoices))

	return data, nil
}

// Helper functions
func getSKUByID(skus []db.CatalogProductSku, id int64) *db.CatalogProductSku {
	for _, sku := range skus {
		if sku.ID == id {
			return &sku
		}
	}
	return nil
}

func getSPUByID(spus []db.CatalogProductSpu, id int64) *db.CatalogProductSpu {
	for _, spu := range spus {
		if spu.ID == id {
			return &spu
		}
	}
	return nil
}

func getCustomerByID(customers []db.AccountCustomer, id int64) *db.AccountCustomer {
	for _, customer := range customers {
		if customer.ID == id {
			return &customer
		}
	}
	return nil
}

func getAccountByID(accounts []db.AccountBase, id int64) *db.AccountBase {
	for _, account := range accounts {
		if account.ID == id {
			return &account
		}
	}
	return nil
}

func getProfileByID(profiles []db.AccountProfile, id int64) *db.AccountProfile {
	for _, profile := range profiles {
		if profile.ID == id {
			return &profile
		}
	}
	return nil
}

func generateRefundReason(fake *faker.Faker) string {
	reasons := []string{
		"Product arrived damaged",
		"Wrong item received",
		"Product doesn't match description",
		"Changed my mind",
		"Found better price elsewhere",
		"Product quality is poor",
		"Shipping took too long",
		"Product doesn't fit",
		"Missing accessories",
		"Product not working properly",
	}
	return reasons[fake.RandomDigit()%len(reasons)]
}

func generateDisputeReason(fake *faker.Faker) string {
	reasons := []string{
		"Customer misunderstood product description",
		"Product was shipped correctly according to specifications",
		"Damage occurred during shipping, not vendor's fault",
		"Customer didn't follow return policy",
		"Product was tested before shipping",
		"Customer changed mind after purchase deadline",
		"Return request beyond acceptable timeframe",
		"Product was delivered to correct address",
	}
	return reasons[fake.RandomDigit()%len(reasons)]
}
