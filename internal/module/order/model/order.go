package ordermodel

import (
	"encoding/json"
	"time"

	commonmodel "shopnexus-remastered/internal/module/common/model"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-remastered/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

const (
	TopicOrderCreated   = "order.created"
	TopicOrderPaid      = "order.paid"
	TopicOrderConfirmed = "order.confirmed"
)

type Order struct {
	ID              uuid.UUID               `json:"id"`
	CustomerID      uuid.UUID               `json:"customer_id"`
	VendorID        uuid.UUID               `json:"vendor_id"`
	ShipmentID      uuid.UUID               `json:"shipment_id"`
	Payment         Payment                 `json:"payment"`
	Status          orderdb.CommonStatus    `json:"status"`
	Address         string                  `json:"address"`
	ProductCost     sharedmodel.Concurrency `json:"product_cost"`
	ShipCost        sharedmodel.Concurrency `json:"ship_cost"`
	ProductDiscount sharedmodel.Concurrency `json:"product_discount"`
	ShipDiscount    sharedmodel.Concurrency `json:"ship_discount"`
	Total           sharedmodel.Concurrency `json:"total"`
	Note            null.String             `json:"note"`
	Data            json.RawMessage         `json:"data"`
	DateCreated     time.Time               `json:"date_created"`
	Items           []orderdb.OrderItem     `json:"items"`
}

type Refund struct {
	ID            uuid.UUID                 `json:"id"`
	AccountID     uuid.UUID                 `json:"account_id"`
	OrderID       uuid.UUID                 `json:"order_id"`
	ConfirmedByID uuid.NullUUID             `json:"confirmed_by_id"`
	ShipmentID    uuid.NullUUID             `json:"shipment_id"`
	Method        orderdb.OrderRefundMethod `json:"method"`
	Status        orderdb.CommonStatus      `json:"status"`
	Reason        string                    `json:"reason"`
	Address       null.String               `json:"address"`
	DateCreated   time.Time                 `json:"date_created"`
	Resources     []commonmodel.Resource    `json:"resources"`
}

type Payment struct {
	ID          int64                   `json:"id"`
	AccountID   uuid.UUID               `json:"account_id"`
	Option      string                  `json:"option"`
	Status      orderdb.CommonStatus    `json:"status"`
	Amount      sharedmodel.Concurrency `json:"amount"`
	Data        json.RawMessage         `json:"data"`
	DateCreated time.Time               `json:"date_created"`
	DatePaid    null.Time               `json:"date_paid"`
	DateExpired time.Time               `json:"date_expired"`
}

type OrderItem struct {
	SkuID    uuid.UUID `json:"sku_id"`
	Quantity int64     `json:"quantity"`
	Note     string    `json:"note"`
}
