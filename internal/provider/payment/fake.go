package payment

import (
	"context"
	"net/url"
	"strconv"
	"time"
	"uuid"
)

// fakeSecret signs the fake's callbacks; anyone may know it, as anyone may
// press its buttons.
const fakeSecret = "dev"

// Fake is VNPay in development and tests. Its payment page is the server's
// dev page at PagesURL/{id}, whose buttons make the callbacks VNPay would,
// signed as VNPay signs them. It knows no payment when asked (querydr) and
// takes every refund. With PAYMENT_VNPAY=fake the server serves the page.
type Fake struct {
	// PagesURL is the dev page's address without the payment's ID.
	PagesURL string
}

var _ Client = Fake{}

// signer is VNPay's signing under the fake's terminal and secret.
func (Fake) signer() VNPayClient {
	return VNPayClient{TmnCode: "SNFAKE01", HashSecret: fakeSecret, Name: FakeVNPay}
}

func (Fake) Gateway() Gateway { return FakeVNPay }

func (f Fake) PayURL(o Order) (string, error) { return f.PagesURL + "/" + o.ID.String(), nil }

func (f Fake) Verify(q url.Values) (Result, error) { return f.signer().Verify(q) }

func (Fake) Query(context.Context, Lookup, time.Time) (Result, error) { return Result{}, ErrNotFound }

func (Fake) Refund(context.Context, Lookup, time.Time) error { return nil }

// Callback is the query VNPay sends, to the IPN and the return page, once
// the payer pays (paid) or cancels the payment id of amount at now.
func (f Fake) Callback(id uuid.UUID, amount int64, paid bool, now time.Time) url.Values {
	code, status, no := ResultCancelled, "02", "0"
	if paid {
		code, status, no = resultPaid, resultPaid, strconv.FormatInt(now.UnixMilli()%100_000_000, 10)
	}
	return f.signer().Signed(map[string]string{
		keyTmnCode: f.signer().TmnCode, keyAmount: strconv.FormatInt(amount*100, 10), keyBankCode: "NCB",
		"vnp_BankTranNo": "FAKE" + no, "vnp_CardType": "ATM", "vnp_PayDate": stamp(now),
		keyOrderInfo: "ShopNexus", keyTransactionNo: no, keyResponseCode: code,
		keyTransactionStatus: status, keyTxnRef: Ref(id),
	})
}
