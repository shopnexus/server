package ordermodel

import (
	"shopnexus-remastered/internal/db"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"time"

	"github.com/guregu/null/v6"
)

const (
	TopicOrderCreated   = "order.created"
	TopicOrderPaid      = "order.paid"
	TopicOrderConfirmed = "order.confirmed"
)

type Order struct {
	ID            int64           `json:"id"`
	AccountID     int64           `json:"account_id"`
	PaymentOption string          `json:"payment_option"`
	PaymentStatus db.SharedStatus `json:"payment_status"`
	Address       string          `json:"address"`
	DateCreated   time.Time       `json:"date_created"`
	DateUpdated   time.Time       `json:"date_updated"`
	Items         []db.OrderItem  `json:"items"`
}

type Refund struct {
	ID           int64                  `json:"id"`
	AccountID    int64                  `json:"account_id"`
	OrderItemID  int64                  `json:"order_item_id"`
	ReviewedByID null.Int64             `json:"reviewed_by_id"`
	ShipmentID   null.Int64             `json:"shipment_id"`
	Method       db.OrderRefundMethod   `json:"method"`
	Status       db.SharedStatus        `json:"status"`
	Reason       string                 `json:"reason"`
	Address      null.String            `json:"address"`
	DateCreated  time.Time              `json:"date_created"`
	Resources    []sharedmodel.Resource `json:"resources"`
}
