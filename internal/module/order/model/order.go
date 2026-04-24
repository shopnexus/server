package ordermodel

import (
	"encoding/json"
	"time"

	commonmodel "shopnexus-server/internal/module/common/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	TopicOrderCreated   = "order_created"
	TopicOrderPaid      = "order_paid"
	TopicOrderConfirmed = "order_confirmed"
)

type OrderItem struct {
	ID                    int64                  `json:"id"`
	OrderID               *uuid.UUID             `json:"order_id"`
	AccountID             uuid.UUID              `json:"account_id"`
	SellerID              uuid.UUID              `json:"seller_id"`
	Address               string                 `json:"address"`
	SkuID                 uuid.UUID              `json:"sku_id"`
	SpuID                 uuid.UUID              `json:"spu_id"`
	SkuName               string                 `json:"sku_name"`
	Quantity              int64                  `json:"quantity"`
	UnitPrice             int64                  `json:"unit_price"`
	PaidAmount            int64                  `json:"paid_amount"`
	Note                  *string                `json:"note"`
	SerialIds             json.RawMessage        `json:"serial_ids"`
	TransportOption       string                 `json:"transport_option"`
	TransportCostEstimate int64                  `json:"transport_cost_estimate"`
	PaymentID             *int64                 `json:"payment_id,omitempty"`
	DateCancelled         *time.Time             `json:"date_cancelled,omitempty"`
	DateCreated           time.Time              `json:"date_created"`
	Resources             []commonmodel.Resource `json:"resources"`
}

type Order struct {
	ID              uuid.UUID       `json:"id"`
	BuyerID         uuid.UUID       `json:"buyer_id"`
	SellerID        uuid.UUID       `json:"seller_id"`
	Transport       *Transport      `json:"transport"`
	Payment         *Payment        `json:"payment"`
	Address         string          `json:"address"`
	ProductCost     int64           `json:"product_cost"`
	ProductDiscount int64           `json:"product_discount"`
	TransportCost   int64           `json:"transport_cost"`
	Total           int64           `json:"total"`
	Note            null.String     `json:"note"`
	Data            json.RawMessage `json:"data"`
	DateCreated     time.Time       `json:"date_created"`
	Items           []OrderItem     `json:"items"`
}

type Payment struct {
	ID              int64               `json:"id"`
	AccountID       uuid.UUID           `json:"account_id"`
	Option          string              `json:"option"`
	PaymentMethodID uuid.NullUUID       `json:"payment_method_id"`
	Status          orderdb.OrderStatus `json:"status"`
	Amount          int64               `json:"amount"`
	Data            json.RawMessage     `json:"data"`
	BuyerCurrency   string              `json:"buyer_currency"`
	SellerCurrency  string              `json:"seller_currency"`
	ExchangeRate    pgtype.Numeric      `json:"exchange_rate"`
	DateCreated     time.Time           `json:"date_created"`
	DatePaid        null.Time           `json:"date_paid"`
	DateExpired     time.Time           `json:"date_expired"`
}

type Transport struct {
	ID          uuid.UUID                    `json:"id"`
	Option      string                       `json:"option"`
	Status      orderdb.NullOrderStatus      `json:"status"`
	Cost        int64                        `json:"cost"`
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
	ItemIDs       []int64                   `json:"item_ids,omitempty"`
	Amount        int64                     `json:"amount,omitempty"`
	DateCreated   time.Time                 `json:"date_created"`
	Resources     []commonmodel.Resource    `json:"resources"`
}

type RefundDispute struct {
	ID          uuid.UUID           `json:"id"`
	RefundID    uuid.UUID           `json:"refund_id"`
	IssuedByID  uuid.UUID           `json:"issued_by_id"`
	Reason      string              `json:"reason"`
	Status      orderdb.OrderStatus `json:"status"`
	DateCreated time.Time           `json:"date_created"`
	DateUpdated time.Time           `json:"date_updated"`
}
