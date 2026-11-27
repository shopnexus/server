package payment_test

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/provider/payment"
)

func sign(secret, data string) string {
	m := hmac.New(sha512.New, []byte(secret))
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

// The pay URL carries the sorted, PHP-encoded parameters and their
// HMAC-SHA512, amounts ×100 and dates in GMT+7.
func TestVNPayPayURL(t *testing.T) {
	t.Parallel()
	v := payment.VNPayClient{TmnCode: "TMN01", HashSecret: "secret", PayPage: "https://pay.test/vpcpay.html", ReturnURL: "https://api.test/r", Name: payment.VNPay}
	id := uuid.MustParse("01a21bbc-e287-7ef6-8327-39161ffc0d61")
	at := time.Date(2026, 11, 27, 2, 0, 0, 0, time.UTC)
	got, err := v.PayURL(payment.Order{ID: id, Amount: 15_000, Info: "ShopNexus boost", IP: "203.0.113.9", CreatedAt: at, ExpiresAt: at.Add(15 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	page, q, _ := strings.Cut(got, "?")
	data, hash, _ := strings.Cut(q, "&vnp_SecureHash=")
	if page != v.PayPage || hash != sign("secret", data) {
		t.Fatalf("url %s", got)
	}
	for _, want := range []string{
		"vnp_Amount=1500000", "vnp_CreateDate=20261127090000", "vnp_ExpireDate=20261127091500", "vnp_OrderInfo=ShopNexus+boost",
		"vnp_ReturnUrl=https%3A%2F%2Fapi.test%2Fr", "vnp_TxnRef=01a21bbce2877ef6832739161ffc0d61", "vnp_Locale=vn",
	} {
		if !strings.Contains(data, want) {
			t.Errorf("no %s in %s", want, data)
		}
	}
	if back, ok := payment.IDOf(payment.Ref(id)); !ok || back != id {
		t.Fatalf("ref %v %v", back, ok)
	}
	if _, ok := payment.IDOf("nope"); ok {
		t.Fatal("read a bad ref")
	}
}

// A callback checks only with its terminal's secret and code.
func TestVerifyCallback(t *testing.T) {
	t.Parallel()
	fake := payment.Fake{PagesURL: "http://api.test/v1/dev/payments"}
	id := uuid.NewV7()
	q := fake.Callback(id, 15_000, true, time.Now())
	r, err := fake.Verify(q)
	if err != nil || !r.Paid || r.Amount != 15_000 || r.Ref != payment.Ref(id) || r.TransactionNo == "" {
		t.Fatalf("paid %+v %v", r, err)
	}
	if r, _ = fake.Verify(fake.Callback(id, 15_000, false, time.Now())); r.Paid || r.ResponseCode != payment.ResultCancelled {
		t.Fatalf("cancelled %+v", r)
	}
	tampered := maps.Clone(q)
	tampered.Set("vnp_Amount", "100")
	if _, err = fake.Verify(tampered); !errors.Is(err, payment.ErrBadSignature) {
		t.Fatalf("tampered: %v", err)
	}
	other := payment.VNPayClient{TmnCode: "SNFAKE01", HashSecret: "another", Name: payment.VNPay}
	if _, err = other.Verify(q); !errors.Is(err, payment.ErrBadSignature) {
		t.Fatalf("another secret: %v", err)
	}
}

// querydr posts signed JSON; an answer's signature is checked, 91 is not
// found and another code a refusal.
func TestVNPayMerchantAPI(t *testing.T) {
	t.Parallel()
	var code string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["vnp_SecureHash"] == "" || body["vnp_TmnCode"] != "TMN01" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		a := map[string]string{
			"vnp_ResponseId": "r1", "vnp_Command": body["vnp_Command"], "vnp_ResponseCode": code, "vnp_Message": "m",
			"vnp_TmnCode": "TMN01", "vnp_TxnRef": body["vnp_TxnRef"], "vnp_Amount": "1500000", "vnp_BankCode": "NCB",
			"vnp_PayDate": "20261127090000", "vnp_TransactionNo": "77", "vnp_TransactionType": "01", "vnp_TransactionStatus": "00",
			"vnp_OrderInfo": "ShopNexus", "vnp_PromotionCode": "", "vnp_PromotionAmount": "",
		}
		fields := []string{
			"vnp_ResponseId", "vnp_Command", "vnp_ResponseCode", "vnp_Message", "vnp_TmnCode", "vnp_TxnRef", "vnp_Amount",
			"vnp_BankCode", "vnp_PayDate", "vnp_TransactionNo", "vnp_TransactionType", "vnp_TransactionStatus", "vnp_OrderInfo",
			"vnp_PromotionCode", "vnp_PromotionAmount",
		}
		data := make([]string, len(fields))
		for i, f := range fields {
			data[i] = a[f]
		}
		a["vnp_SecureHash"] = sign("secret", strings.Join(data, "|"))
		_ = json.NewEncoder(w).Encode(a)
	}))
	t.Cleanup(srv.Close)
	v := payment.VNPayClient{TmnCode: "TMN01", HashSecret: "secret", APIURL: srv.URL, ServerIP: "198.51.100.1", HTTP: srv.Client(), Name: payment.VNPay}
	p := payment.Lookup{ID: uuid.NewV7(), Amount: 15_000, Info: "ShopNexus", TransactionNo: "77", CreatedAt: time.Now()}
	code = "00"
	r, err := v.Query(t.Context(), p, time.Now())
	if err != nil || !r.Paid || r.Amount != 15_000 || r.TransactionNo != "77" {
		t.Fatalf("querydr %+v %v", r, err)
	}
	code = "91"
	if _, err = v.Query(t.Context(), p, time.Now()); !errors.Is(err, payment.ErrNotFound) {
		t.Fatalf("not found: %v", err)
	}
	code = "95"
	if _, err = v.Query(t.Context(), p, time.Now()); !errors.Is(err, payment.ErrRefused) {
		t.Fatalf("refused: %v", err)
	}
}
