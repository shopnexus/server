package ordermodel

import (
	"encoding/json"
	"time"

	commonmodel "shopnexus-server/internal/module/common/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

const (
	TopicOrderCreated   = "order_created"
	TopicOrderPaid      = "order_paid"
	TopicOrderConfirmed = "order_confirmed"
)

type OrderItem struct {
	ID          int64                   `json:"id"`
	OrderID     *uuid.UUID              `json:"order_id"`
	AccountID   uuid.UUID               `json:"account_id"`
	SellerID    uuid.UUID               `json:"seller_id"`
	Address     string                  `json:"address"`
	Status      orderdb.OrderItemStatus `json:"status"`
	SkuID       uuid.UUID               `json:"sku_id"`
	SpuID       uuid.UUID               `json:"spu_id"`
	SkuName     string                  `json:"sku_name"`
	Quantity    int64                   `json:"quantity"`
	UnitPrice   sharedmodel.Concurrency `json:"unit_price"`
	PaidAmount  int64                   `json:"paid_amount"`
	Note        *string                 `json:"note"`
	SerialIds   json.RawMessage         `json:"serial_ids"`
	DateCreated time.Time               `json:"date_created"`
	Resources   []commonmodel.Resource  `json:"resources"`
}

type Order struct {
	ID              uuid.UUID               `json:"id"`
	BuyerID         uuid.UUID               `json:"buyer_id"`
	SellerID        uuid.UUID               `json:"seller_id"`
	Transport       *Transport              `json:"transport"`
	Payment         *Payment                `json:"payment"`
	Address         string                  `json:"address"`
	ProductCost     sharedmodel.Concurrency `json:"product_cost"`
	ProductDiscount sharedmodel.Concurrency `json:"product_discount"`
	TransportCost   sharedmodel.Concurrency `json:"transport_cost"`
	Total           sharedmodel.Concurrency `json:"total"`
	Note            null.String             `json:"note"`
	Data            json.RawMessage         `json:"data"`
	DateCreated     time.Time               `json:"date_created"`
	Items           []OrderItem             `json:"items"`
}

type Payment struct {
	ID              int64                   `json:"id"`
	AccountID       uuid.UUID               `json:"account_id"`
	Option          string                  `json:"option"`
	PaymentMethodID *uuid.UUID              `json:"payment_method_id,omitempty"`
	Status          orderdb.OrderStatus     `json:"status"`
	Amount          sharedmodel.Concurrency `json:"amount"`
	Data            json.RawMessage         `json:"data"`
	DateCreated     time.Time               `json:"date_created"`
	DatePaid        *time.Time              `json:"date_paid"`
	DateExpired     time.Time               `json:"date_expired"`
}

type Transport struct {
	ID          uuid.UUID                    `json:"id"`
	Option      string                       `json:"option"`
	Status      orderdb.OrderTransportStatus `json:"status"`
	Cost        sharedmodel.Concurrency      `json:"cost"`
	Data        json.RawMessage              `json:"data"`
	DateCreated time.Time                    `json:"date_created"`
}

type Refund struct {
	ID            uuid.UUID                 `json:"id"`
	AccountID     uuid.UUID                 `json:"account_id"`
	OrderID       uuid.UUID                 `json:"order_id"`
	ConfirmedByID *uuid.UUID                `json:"confirmed_by_id"`
	TransportID   *uuid.UUID                `json:"transport_id"`
	Method        orderdb.OrderRefundMethod `json:"method"`
	Status        orderdb.OrderStatus       `json:"status"`
	Reason        string                    `json:"reason"`
	Address       *string                   `json:"address"`
	DateCreated   time.Time                 `json:"date_created"`
	Resources     []commonmodel.Resource    `json:"resources"`
}
