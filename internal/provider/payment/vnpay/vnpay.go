package vnpay

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/labstack/echo/v4"
)

// Type guard
var _ payment.Client = (*ClientImpl)(nil)

const (
	MethodQR   = "qr"
	MethodBank = "bank"
	MethodATM  = "atm"
)

type ClientImpl struct {
	config sharedmodel.OptionConfig
	method string

	tmnCode    string
	hashSecret string
	returnURL  string

	handlers []payment.ResultHandler
}

type ClientOptions struct {
	TmnCode    string
	HashSecret string
	ReturnURL  string
}

func NewClients(cfg ClientOptions) []*ClientImpl {
	var clients []*ClientImpl

	methods := []string{MethodQR, MethodBank, MethodATM}
	for _, method := range methods {
		clients = append(clients, &ClientImpl{
			config: sharedmodel.OptionConfig{
				ID:       "vnpay_" + method,
				Provider: "vnpay",
				Name:     "VNPay - " + method,
			},
			method:     method,
			tmnCode:    cfg.TmnCode,
			hashSecret: cfg.HashSecret,
			returnURL:  cfg.ReturnURL,
		})
	}

	return clients
}

func (c *ClientImpl) Config() sharedmodel.OptionConfig {
	return c.config
}

func (c *ClientImpl) Create(ctx context.Context, params payment.CreateParams) (payment.CreateResult, error) {
	var zero payment.CreateResult

	req, err := http.NewRequest("GET", "https://sandbox.vnpayment.vn/paymentv2/vpcpay.html", nil)
	if err != nil {
		return zero, err
	}

	returnURL := c.returnURL
	if params.ReturnURL != "" {
		returnURL = params.ReturnURL
	}

	q := req.URL.Query()
	q.Add("vnp_Version", "2.1.0")
	q.Add("vnp_Command", "pay")
	q.Add("vnp_TmnCode", c.tmnCode)
	// TODO: add currency conversion in concurrency struct, currently hard coded 27000
	q.Add("vnp_Amount", fmt.Sprintf("%.0f", params.Amount.Mul(100).Mul(27000).Float64()))
	q.Add("vnp_CreateDate", formatTime(time.Now()))
	q.Add("vnp_CurrCode", "VND")
	q.Add("vnp_IpAddr", "192.168.1.1")
	q.Add("vnp_Locale", "vn")
	q.Add("vnp_OrderInfo", params.Description)
	q.Add("vnp_OrderType", "billpayment")
	q.Add("vnp_ReturnUrl", returnURL)
	q.Add("vnp_ExpireDate", formatTime(time.Now().Add(30*time.Minute)))
	q.Add("vnp_TxnRef", fmt.Sprintf("%d", params.RefID))

	encodedQuery := q.Encode()
	secureHash := sign(encodedQuery, []byte(c.hashSecret))
	redirectURL := req.URL.String() + "?" + encodedQuery + "&vnp_SecureHash=" + secureHash

	return payment.CreateResult{
		ProviderID:  fmt.Sprintf("%d", params.RefID),
		RedirectURL: redirectURL,
	}, nil
}

func (c *ClientImpl) Get(ctx context.Context, providerID string) (payment.PaymentInfo, error) {
	// VNPay sandbox doesn't have a query API — status comes via IPN only
	return payment.PaymentInfo{
		ProviderID: providerID,
		Status:     payment.StatusPending,
	}, nil
}

func (c *ClientImpl) OnResult(fn payment.ResultHandler) {
	c.handlers = append(c.handlers, fn)
}

func (c *ClientImpl) Charge(ctx context.Context, params payment.ChargeParams) (payment.ChargeResult, error) {
	return payment.ChargeResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Refund(ctx context.Context, params payment.RefundParams) (payment.RefundResult, error) {
	return payment.RefundResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Tokenize(ctx context.Context, params payment.TokenizeParams) (payment.TokenizeResult, error) {
	return payment.TokenizeResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) InitializeWebhook(e *echo.Echo) {
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
		hash := sign(hashData, []byte(c.hashSecret))
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

		result := payment.WebhookResult{
			RefID:  txnRef,
			Status: status,
		}

		// Fan-out to all registered handlers
		ctx := r.Context()
		for _, fn := range c.handlers {
			if err := fn(ctx, result); err != nil {
				slog.Error("vnpay webhook: handler error", slog.Any("error", err))
			}
		}

		return ec.NoContent(http.StatusOK)
	})
}
