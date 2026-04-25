package ordermodel

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/shopspring/decimal"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
)

// Transaction is the domain-layer representation of an entry in the order.transaction ledger.
type Transaction struct {
	ID            int64
	FromID        uuid.NullUUID
	ToID          uuid.NullUUID
	Type          string
	Status        orderdb.OrderStatus
	Note          string
	PaymentOption null.String
	WalletID      uuid.NullUUID
	Data          json.RawMessage

	Amount       int64
	FromCurrency string
	ToCurrency   string
	ExchangeRate decimal.Decimal

	DateCreated time.Time
	DatePaid    null.Time
	DateExpired time.Time
}

// Transport is the domain-layer representation of a shipping record.
type Transport struct {
	ID          int64
	Option      string
	Status      orderdb.NullOrderStatus
	Data        json.RawMessage
	DateCreated time.Time
}

// OrderItem is the domain-layer item (pre- and post-confirmation).
type OrderItem struct {
	ID        int64
	OrderID   uuid.NullUUID
	AccountID uuid.UUID
	SellerID  uuid.UUID
	SkuID     uuid.UUID
	SpuID     uuid.UUID
	SkuName   string
	Address   string
	Note      null.String
	SerialIDs json.RawMessage

	Quantity        int64
	TransportOption string
	SubtotalAmount  int64
	PaidAmount      int64
	PaymentTxID     int64

	DateCreated   time.Time
	DateCancelled null.Time
	CancelledByID uuid.NullUUID
	RefundTxID    null.Int
}

// Order is the domain-layer confirmed order (exists only after seller confirm).
type Order struct {
	ID          uuid.UUID
	BuyerID     uuid.UUID
	SellerID    uuid.UUID
	TransportID int64
	Address     string
	DateCreated time.Time

	ConfirmedByID uuid.UUID
	SellerTxID    int64
	Note          null.String

	// Derived (optional loaded):
	TotalAmount  int64
	Items        []OrderItem
	Transport    *Transport
	ConfirmFeeTx *Transaction
	PayoutTx     *Transaction
}

// Refund is the 2-stage refund request.
type Refund struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	OrderItemID int64
	TransportID int64
	Method      orderdb.OrderRefundMethod
	Reason      string
	Address     null.String
	DateCreated time.Time
	Status      orderdb.OrderStatus

	AcceptedByID  uuid.NullUUID
	DateAccepted  null.Time
	RejectionNote null.String

	ApprovedByID uuid.NullUUID
	DateApproved null.Time
	RefundTxID   null.Int
}

// RefundDispute is the domain-layer dispute raised against a refund decision.
type RefundDispute struct {
	ID           uuid.UUID
	AccountID    uuid.UUID
	RefundID     uuid.UUID
	Reason       string
	Status       orderdb.OrderStatus
	Note         string
	DateCreated  time.Time
	ResolvedByID uuid.NullUUID
	DateResolved null.Time
}
