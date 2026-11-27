package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"
)

// version is the VNPay API version the client speaks.
const version = "2.1.0"

// VNPayClient is VNPay's payment page (vnp_Command pay), its signed
// callbacks and its merchant API's querydr, for one terminal.
// PayURL is the page's address (…/paymentv2/vpcpay.html), APIURL the
// merchant API's (…/merchant_webapi/api/transaction), ReturnURL where VNPay
// sends the payer back, and ServerIP the address the merchant API calls
// come from. HashSecret is never logged.
type VNPayClient struct {
	TmnCode, HashSecret string
	PayPage, APIURL     string
	ReturnURL           string
	ServerIP            string
	HTTP                *http.Client
	// Name is the gateway the payments are kept under; vnpay, or fake for
	// Fake, which signs as VNPay does.
	Name Gateway
}

var _ Client = VNPayClient{}

func (v VNPayClient) Gateway() Gateway { return v.Name }

// sign is HMAC-SHA512 of data under the hash secret, in hex.
func (v VNPayClient) sign(data string) string {
	m := hmac.New(sha512.New, []byte(v.HashSecret))
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

// encode is PHP's urlencode, which VNPay's checksum follows: every byte but
// letters, digits and -_. escaped, a space as +.
func encode(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// query is the vnp_ parameters of params sorted by name and encoded, the
// text both the URL and the checksum are made of.
func query(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = encode(k) + "=" + encode(params[k])
	}
	return strings.Join(parts, "&")
}

// Params are the vnp_ parameters of a payment page for o.
func (v VNPayClient) Params(o Order) map[string]string {
	locale := "vn"
	if o.English {
		locale = "en"
	}
	return map[string]string{
		keyVersion: version, keyCommand: "pay", keyTmnCode: v.TmnCode,
		keyAmount: strconv.FormatInt(o.Amount*100, 10), "vnp_CurrCode": "VND",
		keyTxnRef: Ref(o.ID), keyOrderInfo: o.Info, "vnp_OrderType": "other",
		"vnp_Locale": locale, "vnp_ReturnUrl": v.ReturnURL, keyIPAddr: o.IP,
		keyCreateDate: stamp(o.CreatedAt), "vnp_ExpireDate": stamp(o.ExpiresAt),
	}
}

// PayURL is VNPay's page with o's parameters and their vnp_SecureHash.
func (v VNPayClient) PayURL(o Order) (string, error) {
	q := query(v.Params(o))
	return v.PayPage + "?" + q + "&vnp_SecureHash=" + v.sign(q), nil
}

// Signed is params with their vnp_SecureHash, as VNPay sends a callback.
func (v VNPayClient) Signed(params map[string]string) url.Values {
	q := query(params)
	out := url.Values{}
	for k, val := range params {
		out.Set(k, val)
	}
	out.Set(keySecureHash, v.sign(q))
	return out
}

// Verify checks a callback's vnp_SecureHash over its other vnp_ parameters,
// then reads the result.
func (v VNPayClient) Verify(q url.Values) (Result, error) {
	params := map[string]string{}
	for k := range q {
		if strings.HasPrefix(k, "vnp_") && k != keySecureHash && k != "vnp_SecureHashType" {
			params[k] = q.Get(k)
		}
	}
	if !hmac.Equal([]byte(strings.ToLower(q.Get(keySecureHash))), []byte(v.sign(query(params)))) {
		return Result{}, ErrBadSignature
	}
	if q.Get(keyTmnCode) != v.TmnCode {
		return Result{}, ErrBadSignature
	}
	amount, err := strconv.ParseInt(q.Get(keyAmount), 10, 64)
	if err != nil || amount%100 != 0 {
		return Result{}, fmt.Errorf("read vnp_Amount %q: %w", q.Get(keyAmount), ErrBadSignature)
	}
	r := Result{
		Ref: q.Get(keyTxnRef), Amount: amount / 100, ResponseCode: q.Get(keyResponseCode),
		TransactionStatus: q.Get(keyTransactionStatus), TransactionNo: q.Get(keyTransactionNo), BankCode: q.Get(keyBankCode),
	}
	r.Paid = r.ResponseCode == resultPaid && r.TransactionStatus == resultPaid
	return r, nil
}

// apiReply is the merchant API's answer, querydr's fields.
//
//nolint:tagliatelle // VNPay's own names.
type apiReply struct {
	ResponseID        string `json:"vnp_ResponseId"`
	Command           string `json:"vnp_Command"`
	ResponseCode      string `json:"vnp_ResponseCode"`
	Message           string `json:"vnp_Message"`
	TmnCode           string `json:"vnp_TmnCode"`
	TxnRef            string `json:"vnp_TxnRef"`
	Amount            string `json:"vnp_Amount"`
	BankCode          string `json:"vnp_BankCode"`
	PayDate           string `json:"vnp_PayDate"`
	TransactionNo     string `json:"vnp_TransactionNo"`
	TransactionType   string `json:"vnp_TransactionType"`
	TransactionStatus string `json:"vnp_TransactionStatus"`
	OrderInfo         string `json:"vnp_OrderInfo"`
	PromotionCode     string `json:"vnp_PromotionCode"`
	PromotionAmount   string `json:"vnp_PromotionAmount"`
	SecureHash        string `json:"vnp_SecureHash"`
}

// call posts a merchant API request, signed over the fields named in
// signed in that order, and checks the answer's signature over the fields
// answer names.
func (v VNPayClient) call(ctx context.Context, body map[string]string, signed []string, answer func(apiReply) []string) (apiReply, error) {
	data := make([]string, len(signed))
	for i, k := range signed {
		data[i] = body[k]
	}
	body[keySecureHash] = v.sign(strings.Join(data, "|"))
	b, err := json.Marshal(body)
	if err != nil {
		return apiReply{}, fmt.Errorf("encode VNPay %s: %w", body[keyCommand], err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.APIURL, bytes.NewReader(b))
	if err != nil {
		return apiReply{}, fmt.Errorf("build VNPay %s: %w", body[keyCommand], err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.HTTP.Do(req)
	if err != nil {
		return apiReply{}, fmt.Errorf("call VNPay %s: %w", body[keyCommand], err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return apiReply{}, fmt.Errorf("read VNPay %s: %w", body[keyCommand], err)
	}
	if resp.StatusCode != http.StatusOK {
		return apiReply{}, fmt.Errorf("VNPay %s: HTTP %d", body[keyCommand], resp.StatusCode)
	}
	var r apiReply
	if err = json.Unmarshal(raw, &r); err != nil {
		return apiReply{}, fmt.Errorf("decode VNPay %s: %w", body[keyCommand], err)
	}
	// An answer that refuses the request may come unsigned.
	if r.ResponseCode == resultPaid && !hmac.Equal([]byte(strings.ToLower(r.SecureHash)), []byte(v.sign(strings.Join(answer(r), "|")))) {
		return apiReply{}, fmt.Errorf("VNPay %s: %w", body[keyCommand], ErrBadSignature)
	}
	return r, nil
}

// request is the fields every merchant API request has.
func (v VNPayClient) request(command string, p Lookup, now time.Time) map[string]string {
	return map[string]string{
		keyRequestID: strings.ReplaceAll(uuid.NewV7().String(), "-", ""), keyVersion: version, keyCommand: command,
		keyTmnCode: v.TmnCode, keyTxnRef: Ref(p.ID), keyOrderInfo: p.Info,
		keyTransactionDate: stamp(p.CreatedAt), keyCreateDate: stamp(now), keyIPAddr: v.ServerIP,
	}
}

// Query is querydr. VNPay's 91 is ErrNotFound.
func (v VNPayClient) Query(ctx context.Context, p Lookup, now time.Time) (Result, error) {
	body := v.request("querydr", p, now)
	if p.TransactionNo != "" {
		body[keyTransactionNo] = p.TransactionNo
	}
	r, err := v.call(ctx, body,
		[]string{keyRequestID, keyVersion, keyCommand, keyTmnCode, keyTxnRef, keyTransactionDate, keyCreateDate, keyIPAddr, keyOrderInfo},
		func(r apiReply) []string {
			return []string{
				r.ResponseID, r.Command, r.ResponseCode, r.Message, r.TmnCode, r.TxnRef, r.Amount, r.BankCode, r.PayDate,
				r.TransactionNo, r.TransactionType, r.TransactionStatus, r.OrderInfo, r.PromotionCode, r.PromotionAmount,
			}
		})
	if err != nil {
		return Result{}, err
	}
	switch r.ResponseCode {
	case resultPaid:
	case "91":
		return Result{}, ErrNotFound
	default:
		return Result{}, RefusedError{Code: r.ResponseCode, Message: r.Message}
	}
	amount, err := strconv.ParseInt(r.Amount, 10, 64)
	if err != nil {
		return Result{}, fmt.Errorf("read querydr's vnp_Amount %q: %w", r.Amount, err)
	}
	out := Result{
		Ref: r.TxnRef, Amount: amount / 100, TransactionStatus: r.TransactionStatus, TransactionNo: r.TransactionNo, BankCode: r.BankCode,
		ResponseCode: r.TransactionStatus, Paid: r.TransactionStatus == resultPaid,
	}
	return out, nil
}
