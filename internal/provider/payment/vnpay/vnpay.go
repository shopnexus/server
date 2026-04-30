package vnpay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/labstack/echo/v4"
)

var _ payment.Client = (*ClientImpl)(nil)

const (
	MethodQR   = "qr"
	MethodBank = "bank"
	MethodATM  = "atm"
)

type Data struct {
	TmnCode    string `json:"tmn_code"`
	HashSecret string `json:"hash_secret"`
	ReturnURL  string `json:"return_url"`
	Method     string `json:"method"`
}

type ClientImpl struct {
	config sharedmodel.Option
	data   Data
}

func NewClient(cfg sharedmodel.Option) payment.Client {
	var data Data
	if len(cfg.Data) > 0 {
		_ = json.Unmarshal(cfg.Data, &data)
	}
	return &ClientImpl{config: cfg, data: data}
}

func (c *ClientImpl) Config() sharedmodel.Option {
	return c.config
}

func (c *ClientImpl) Charge(ctx context.Context, params payment.ChargeParams) (payment.ChargeResult, error) {
	var zero payment.ChargeResult

	req, err := http.NewRequest(http.MethodGet, "https://sandbox.vnpayment.vn/paymentv2/vpcpay.html", nil)
	if err != nil {
		return zero, err
	}

	returnURL := c.data.ReturnURL
	if params.ReturnURL != "" {
		returnURL = params.ReturnURL
	}

	q := req.URL.Query()
	q.Add("vnp_Version", "2.1.0")
	q.Add("vnp_Command", "pay")
	q.Add("vnp_TmnCode", c.data.TmnCode)
	// VNPay amount is in xu (VND * 100)
	q.Add("vnp_Amount", fmt.Sprintf("%d", params.Amount*100))
	q.Add("vnp_CreateDate", formatTime(time.Now()))
	q.Add("vnp_CurrCode", "VND")
	q.Add("vnp_IpAddr", "192.168.1.1")
	q.Add("vnp_Locale", "vn")
	q.Add("vnp_OrderInfo", params.Description)
	q.Add("vnp_OrderType", "billpayment")
	q.Add("vnp_ReturnUrl", returnURL)
	q.Add("vnp_ExpireDate", formatTime(time.Now().Add(30*time.Minute)))
	q.Add("vnp_TxnRef", params.RefID)

	encodedQuery := q.Encode()
	secureHash := sign(encodedQuery, []byte(c.data.HashSecret))

	return payment.ChargeResult{
		ProviderID:  params.RefID,
		RedirectURL: req.URL.String() + "?" + encodedQuery + "&vnp_SecureHash=" + secureHash,
		Status:      payment.StatusPending,
	}, nil
}

func (c *ClientImpl) Refund(ctx context.Context, params payment.RefundParams) (payment.RefundResult, error) {
	return payment.RefundResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Tokenize(ctx context.Context, params payment.TokenizeParams) (payment.TokenizeResult, error) {
	return payment.TokenizeResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) WireWebhooks(e *echo.Echo, deliver payment.NotificationHandler, registered map[string]struct{}) string {
	const key = "payment/vnpay"
	if _, ok := registered[key]; ok {
		return key
	}
	e.GET("/api/v1/payment/webhook/vnpay", func(ec echo.Context) error {
		r := ec.Request()
		if err := r.ParseForm(); err != nil {
			slog.Error("vnpay webhook: parse form", slog.Any("error", err))
			return ec.NoContent(http.StatusBadRequest)
		}

		params := make(map[string]any, len(r.Form))
		for key, values := range r.Form {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}

		expectedHash, ok := params["vnp_SecureHash"].(string)
		if !ok {
			slog.Error("vnpay webhook: missing vnp_SecureHash")
			return ec.NoContent(http.StatusBadRequest)
		}
		delete(params, "vnp_SecureHash")
		delete(params, "vnp_SecureHashType")

		hashData := buildSortedQuery(params)
		hash := sign(hashData, []byte(c.data.HashSecret))
		if hash != expectedHash {
			slog.Error("vnpay webhook: hash mismatch")
			return ec.NoContent(http.StatusBadRequest)
		}

		txnRef, _ := params["vnp_TxnRef"].(string)
		if txnRef == "" {
			slog.Error("vnpay webhook: missing vnp_TxnRef")
			return ec.NoContent(http.StatusBadRequest)
		}

		responseCode, _ := params["vnp_ResponseCode"].(string)
		status := payment.StatusFailed
		if responseCode == "00" {
			status = payment.StatusSuccess
		}

		notification := payment.Notification{
			RefID:  txnRef,
			Status: status,
		}

		if err := deliver(r.Context(), notification); err != nil {
			slog.Error("vnpay webhook: deliver error", slog.Any("error", err))
		}

		return ec.NoContent(http.StatusOK)
	})
	return key
}
