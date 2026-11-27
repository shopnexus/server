// Package domain holds module payment's entity: one payment of one of the
// platform's sales, from the gateway's page to paid or closed. A paid
// payment is never given back.
package domain

import (
	"encoding/json"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// ExpireAfter is how long a payer has to pay; the gateway's page refuses
// the payment after it too.
const ExpireAfter = 15 * time.Minute

var (
	ErrUnavailable = apperr.New(apperr.KindUnavailable, "payment_unavailable", "this server takes no payments")
	ErrNotFound    = apperr.New(apperr.KindNotFound, "payment_not_found", "no such payment")
	// ErrAmountMismatch is a gateway result of another amount than the
	// payment's.
	ErrAmountMismatch = apperr.New(apperr.KindConflict, "payment_amount_mismatch", "the amount differs from the payment's")
)

// Status is where a payment is.
type Status string

const (
	StatusPending Status = "pending"
	StatusPaid    Status = "paid"
	StatusFailed  Status = "failed"
	StatusExpired Status = "expired"
)

// Product is what a payment pays for: the module and its row.
type Product string

const ProductBoost Product = "boost"

// Result is what the gateway reported of a payment, signature checked.
type Result struct {
	Amount        int64
	Paid          bool
	ResultCode    string
	TransactionNo string
	BankCode      string
}

// Outcome is what a result changed.
type Outcome int

const (
	// Unchanged is a result the payment had already.
	Unchanged Outcome = iota
	Paid
	Failed
)

// Payment is one payment. Its fields are its stored state.
type Payment struct{ s State }

// NewPayment starts a payment, pending until the gateway reports it. The
// first write ID is the id.
func NewPayment(id, account uuid.UUID, product Product, productID uuid.UUID, amount int64, gateway, payURL string, now time.Time) Payment {
	return Payment{s: State{
		ID: id, AccountID: account, Product: product, ProductID: productID, Amount: amount, Gateway: gateway,
		Status: StatusPending, PayURL: payURL, CreatedAt: now, ExpiresAt: now.Add(ExpireAfter), WriteID: id,
	}}
}

// Report records the gateway's result once. A success marks the payment
// paid even after it failed or expired: the money arrived, and the
// purchase it was for decides what becomes of it. A failure closes only a
// pending payment. A result of another amount is ErrAmountMismatch.
func (p *Payment) Report(r Result, now time.Time) (Outcome, error) {
	if r.Amount != p.s.Amount {
		return Unchanged, ErrAmountMismatch
	}
	switch {
	case r.Paid && (p.s.Status == StatusPending || p.s.Status == StatusFailed || p.s.Status == StatusExpired):
		p.s.Status, p.s.PaidAt, p.s.ClosedAt = StatusPaid, &now, nil
		p.s.GatewayRef, p.s.BankCode, p.s.ResultCode = r.TransactionNo, r.BankCode, r.ResultCode
		return Paid, nil
	case !r.Paid && p.s.Status == StatusPending:
		p.s.Status, p.s.ClosedAt, p.s.ResultCode, p.s.BankCode = StatusFailed, &now, r.ResultCode, r.BankCode
		return Failed, nil
	default:
		return Unchanged, nil
	}
}

// Expire closes a payment still pending and reports whether it did.
func (p *Payment) Expire(now time.Time) bool {
	if p.s.Status != StatusPending {
		return false
	}
	p.s.Status, p.s.ClosedAt = StatusExpired, &now
	return true
}

func (p *Payment) ID() uuid.UUID        { return p.s.ID }
func (p *Payment) Status() Status       { return p.s.Status }
func (p *Payment) Amount() int64        { return p.s.Amount }
func (p *Payment) WriteID() uuid.UUID   { return p.s.WriteID }
func (p *Payment) CreatedAt() time.Time { return p.s.CreatedAt }

// State is every stored field of a payment. Stores only, and views.
type State struct {
	ID         uuid.UUID  `json:"id"`
	AccountID  uuid.UUID  `json:"account_id"`
	Product    Product    `json:"product"`
	ProductID  uuid.UUID  `json:"product_id"`
	Amount     int64      `json:"amount"`
	Gateway    string     `json:"gateway"`
	Status     Status     `json:"status"`
	PayURL     string     `json:"pay_url"`
	GatewayRef string     `json:"gateway_ref,omitempty"`
	BankCode   string     `json:"bank_code,omitempty"`
	ResultCode string     `json:"result_code,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	PaidAt     *time.Time `json:"paid_at,omitempty"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	WriteID    uuid.UUID  `json:"write_id"`
}

// Restore rebuilds a stored payment. Stores only.
func Restore(st State) Payment { return Payment{s: st} }

// State returns every stored field.
func (p *Payment) State() State { return p.s }

// Saved records that a store wrote p under writeID. Stores only.
func (p *Payment) Saved(writeID uuid.UUID) { p.s.WriteID = writeID }

// MarshalJSON lets a Restate step return a payment for its journal.
func (p Payment) MarshalJSON() ([]byte, error) { return json.Marshal(p.s) }

func (p *Payment) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &p.s) }
