package ordermodel

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/shopspring/decimal"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
)

// PaymentSession is the domain-layer payment intent: one logical money flow
// (checkout, confirmation fee, payout). Has 0..N child Transaction rail movements.
type PaymentSession struct {
	ID             int64               `json:"id"`
	Kind           string              `json:"kind"`
	Status         orderdb.OrderStatus `json:"status"`
	FromID         uuid.NullUUID       `json:"from_id"`
	ToID           uuid.NullUUID       `json:"to_id"`
	Note           string              `json:"note"`
	Currency    string          `json:"currency"`
	TotalAmount int64           `json:"total_amount"`
	Data        json.RawMessage `json:"data"`

	DateCreated time.Time `json:"date_created"`
	DatePaid    null.Time `json:"date_paid"`
	DateExpired time.Time `json:"date_expired"`
}

// Transaction is the domain-layer ledger leg: one rail movement within a payment session.
// Reversals are NEW rows with negative amount + ReversesID pointing to the original.
type Transaction struct {
	ID            int64               `json:"id"`
	SessionID     int64               `json:"session_id"`
	Status        orderdb.OrderStatus `json:"status"`
	Note          string              `json:"note"`
	Error         null.String         `json:"error"`
	PaymentOption null.String         `json:"payment_option"`
	WalletID      uuid.NullUUID       `json:"wallet_id"`
	Data          json.RawMessage     `json:"data"`

	Amount       int64           `json:"amount"`
	FromCurrency string          `json:"from_currency"`
	ToCurrency   string          `json:"to_currency"`
	ExchangeRate decimal.Decimal `json:"exchange_rate"`

	ReversesID null.Int `json:"reverses_id"`

	DateCreated time.Time `json:"date_created"`
	DateSettled null.Time `json:"date_settled"`
	DateExpired null.Time `json:"date_expired"`
}

// Transport is the domain-layer representation of a shipping record.
type Transport struct {
	ID          int64                   `json:"id"`
	Option      string                  `json:"option"`
	Status      orderdb.NullOrderStatus `json:"status"`
	Data        json.RawMessage         `json:"data"`
	DateCreated time.Time               `json:"date_created"`
}

// OrderItem is the domain-layer item (pre- and post-confirmation).
// Refund status is derived from negative-amount transactions in the item's payment session.
type OrderItem struct {
	ID        int64           `json:"id"`
	OrderID   uuid.NullUUID   `json:"order_id"`
	AccountID uuid.UUID       `json:"account_id"`
	SellerID  uuid.UUID       `json:"seller_id"`
	SkuID     uuid.UUID       `json:"sku_id"`
	SpuID     uuid.UUID       `json:"spu_id"`
	SkuName   string          `json:"sku_name"`
	Address   string          `json:"address"`
	Note      null.String     `json:"note"`
	SerialIDs json.RawMessage `json:"serial_ids"`

	Quantity         int64  `json:"quantity"`
	TransportOption  string `json:"transport_option"`
	SubtotalAmount   int64  `json:"subtotal_amount"`
	TotalAmount      int64  `json:"total_amount"`
	PaymentSessionID int64  `json:"payment_session_id"`

	DateCreated   time.Time     `json:"date_created"`
	DateCancelled null.Time     `json:"date_cancelled"`
	CancelledByID uuid.NullUUID `json:"cancelled_by_id"`

	// Derived (optional loaded):
	PaymentSession *PaymentSession `json:"payment_session,omitempty"`
}

// Order is the domain-layer confirmed order (exists only after seller confirm).
type Order struct {
	ID          uuid.UUID `json:"id"`
	BuyerID     uuid.UUID `json:"buyer_id"`
	SellerID    uuid.UUID `json:"seller_id"`
	TransportID int64     `json:"transport_id"`
	Address     string    `json:"address"`
	DateCreated time.Time `json:"date_created"`

	ConfirmedByID    uuid.UUID   `json:"confirmed_by_id"`
	ConfirmSessionID int64       `json:"confirm_session_id"`
	Note             null.String `json:"note"`

	// Derived (optional loaded):
	TotalAmount    int64           `json:"total_amount"`
	Items          []OrderItem     `json:"items"`
	Transport      *Transport      `json:"transport,omitempty"`
	ConfirmSession *PaymentSession `json:"confirm_session,omitempty"`
	PayoutSession  *PaymentSession `json:"payout_session,omitempty"`
}

// Refund is the 2-stage refund request (workflow record). The actual money flow lives
// as a negative-amount transaction in the item's payment session, linked via RefundTxID.
type Refund struct {
	ID          uuid.UUID                 `json:"id"`
	AccountID   uuid.UUID                 `json:"account_id"`
	OrderID     uuid.UUID                 `json:"order_id"`
	TransportID int64                     `json:"transport_id"`
	Method      orderdb.OrderRefundMethod `json:"method"`
	Reason      string                    `json:"reason"`
	Address     null.String               `json:"address"`
	DateCreated time.Time                 `json:"date_created"`
	Status      orderdb.OrderStatus       `json:"status"`

	AcceptedByID  uuid.NullUUID `json:"accepted_by_id"`
	DateAccepted  null.Time     `json:"date_accepted"`
	RejectionNote null.String   `json:"rejection_note"`

	ApprovedByID uuid.NullUUID `json:"approved_by_id"`
	DateApproved null.Time     `json:"date_approved"`
	RefundTxID   null.Int      `json:"refund_tx_id"`
}

// RefundDispute is the domain-layer dispute raised against a refund decision.
type RefundDispute struct {
	ID           uuid.UUID           `json:"id"`
	AccountID    uuid.UUID           `json:"account_id"`
	RefundID     uuid.UUID           `json:"refund_id"`
	Reason       string              `json:"reason"`
	Status       orderdb.OrderStatus `json:"status"`
	Note         string              `json:"note"`
	DateCreated  time.Time           `json:"date_created"`
	ResolvedByID uuid.NullUUID       `json:"resolved_by_id"`
	DateResolved null.Time           `json:"date_resolved"`
}
