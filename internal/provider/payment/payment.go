// Package payment takes a payer to a payment gateway's page and reads what
// the gateway reports: VNPay, written from its public API docs, and Fake,
// which signs as VNPay does and stands in for it in development and tests.
package payment

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"uuid"
)

// Gateway names the gateway a payment went through: vnpay, or fake.
type Gateway string

const (
	VNPay      Gateway = "vnpay"
	FakeVNPay  Gateway = "fake"
	resultPaid         = "00"
	// ResultCancelled is VNPay's response code for a payer who cancelled.
	ResultCancelled = "24"
)

var (
	// ErrBadSignature is a callback whose vnp_SecureHash does not check.
	ErrBadSignature = errors.New("payment: the signature does not check")
	// ErrNotFound is a payment the gateway does not know.
	ErrNotFound = errors.New("payment: the gateway does not know the payment")
	// ErrRefused is a request the gateway refused; the error carries its code.
	ErrRefused = errors.New("payment: the gateway refused the request")
)

// RefusedError is ErrRefused with the gateway's response code and message.
type RefusedError struct{ Code, Message string }

func (e RefusedError) Error() string { return ErrRefused.Error() + ": " + e.Code + " " + e.Message }

func (RefusedError) Is(target error) bool { return target == ErrRefused }

// Order is what a payment page charges. Ref is the order reference the
// gateway keeps (vnp_TxnRef); Info is a line of plain ASCII.
type Order struct {
	ID        uuid.UUID
	Amount    int64 // in đồng
	Info      string
	IP        string
	English   bool
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Ref is the gateway's order reference of a payment: its ID without
// hyphens, which VNPay takes as alphanumeric.
func Ref(id uuid.UUID) string { return strings.ReplaceAll(id.String(), "-", "") }

// IDOf reads a payment's ID back from its Ref; false for another string.
func IDOf(ref string) (uuid.UUID, bool) {
	if len(ref) != 32 {
		return uuid.UUID{}, false
	}
	id, err := uuid.Parse(ref[0:8] + "-" + ref[8:12] + "-" + ref[12:16] + "-" + ref[16:20] + "-" + ref[20:])
	return id, err == nil
}

// Result is a payment's result as the gateway reports it, signature
// checked. Paid is VNPay's response code and transaction status both 00.
type Result struct {
	Ref               string
	Amount            int64 // in đồng
	Paid              bool
	ResponseCode      string
	TransactionStatus string
	TransactionNo     string
	BankCode          string
}

// Lookup names a payment to ask the gateway about, or to refund.
type Lookup struct {
	ID            uuid.UUID
	Amount        int64
	Info          string
	TransactionNo string
	CreatedAt     time.Time
}

// Client is one gateway.
type Client interface {
	// Gateway names the gateway the payments it makes are kept under.
	Gateway() Gateway
	// PayURL is the page the payer pays o on.
	PayURL(o Order) (string, error)
	// Verify checks the signature of a callback's query, the return page's
	// or the IPN's, and reads it; ErrBadSignature when it does not check.
	Verify(q url.Values) (Result, error)
	// Query asks the gateway what became of a payment (querydr);
	// ErrNotFound when it does not know it.
	Query(ctx context.Context, p Lookup, now time.Time) (Result, error)
	// Refund gives a paid payment back in full. A refund the gateway has
	// already taken is no error.
	Refund(ctx context.Context, p Lookup, now time.Time) error
}

// The vnp_ parameters more than one request or answer carries.
const (
	keyAmount            = "vnp_Amount"
	keyOrderInfo         = "vnp_OrderInfo"
	keyTxnRef            = "vnp_TxnRef"
	keyCommand           = "vnp_Command"
	keyIPAddr            = "vnp_IpAddr"
	keyCreateDate        = "vnp_CreateDate"
	keyRequestID         = "vnp_RequestId"
	keyTransactionDate   = "vnp_TransactionDate"
	keyTmnCode           = "vnp_TmnCode"
	keyTransactionNo     = "vnp_TransactionNo"
	keySecureHash        = "vnp_SecureHash"
	keyVersion           = "vnp_Version"
	keyResponseCode      = "vnp_ResponseCode"
	keyTransactionStatus = "vnp_TransactionStatus"
	keyBankCode          = "vnp_BankCode"
	keyTransactionType   = "vnp_TransactionType"
)

// vietnam is the time zone of every date VNPay reads and writes.
var vietnam = time.FixedZone("ICT", 7*60*60)

// stamp is a time as VNPay writes dates: yyyyMMddHHmmss in GMT+7.
func stamp(t time.Time) string { return t.In(vietnam).Format("20060102150405") }
