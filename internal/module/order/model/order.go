package ordermodel

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
)

// Transaction is the domain-layer representation of an entry in the order.transaction ledger.
type Transaction struct {
	ID            int64
	FromID        *uuid.UUID
	ToID          *uuid.UUID
	Type          string
	Status        orderdb.OrderStatus
	Note          string
	PaymentOption *string
	InstrumentID  *uuid.UUID
	Data          json.RawMessage

	Amount       int64
	FromCurrency string
	ToCurrency   string
	ExchangeRate string // pgtype.Numeric surfaced as string for precision

	DateCreated time.Time
	DatePaid    *time.Time
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
	OrderID   *uuid.UUID
	AccountID uuid.UUID
	SellerID  uuid.UUID
	SkuID     uuid.UUID
	SkuName   string
	Address   string
	Note      *string
	SerialIDs json.RawMessage

	Quantity        int64
	TransportOption string
	SubtotalAmount  int64
	PaidAmount      int64
	PaymentTxID     int64

	DateCreated   time.Time
	DateCancelled *time.Time
	CancelledByID *uuid.UUID
	RefundTxID    *int64
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
	Note          *string

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
	Address     *string
	DateCreated time.Time
	Status      orderdb.OrderStatus

	AcceptedByID  *uuid.UUID
	DateAccepted  *time.Time
	RejectionNote *string

	ApprovedByID *uuid.UUID
	DateApproved *time.Time
	RefundTxID   *int64
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
	ResolvedByID *uuid.UUID
	DateResolved *time.Time
}
