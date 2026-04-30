package sepay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/labstack/echo/v4"
)

var _ payment.Client = (*ClientImpl)(nil)

const (
	sandboxCheckoutURL = "https://pay-sandbox.sepay.vn/v1/checkout/init"
	prodCheckoutURL    = "https://pay.sepay.vn/v1/checkout/init"
)

type Data struct {
	MerchantID   string `json:"merchant_id"`
	SecretKey    string `json:"secret_key"`
	IPNSecretKey string `json:"ipn_secret_key"`
	SuccessURL   string `json:"success_url"`
	ErrorURL     string `json:"error_url"`
	CancelURL    string `json:"cancel_url"`
	Sandbox      bool   `json:"sandbox"`
}

type ClientImpl struct {
	config      sharedmodel.Option
	data        Data
	checkoutURL string
}

func NewClient(cfg sharedmodel.Option) payment.Client {
	var data Data
	if len(cfg.Data) > 0 {
		_ = json.Unmarshal(cfg.Data, &data)
	}
	checkoutURL := prodCheckoutURL
	if data.Sandbox {
		checkoutURL = sandboxCheckoutURL
	}
	return &ClientImpl{config: cfg, data: data, checkoutURL: checkoutURL}
}

func (c *ClientImpl) Config() sharedmodel.Option {
	return c.config
}

func (c *ClientImpl) Charge(ctx context.Context, params payment.ChargeParams) (payment.ChargeResult, error) {
	invoiceNumber := params.RefID

	fields := []keyValue{
		{"merchant", c.data.MerchantID},
		{"operation", "PURCHASE"},
		{"payment_method", "BANK_TRANSFER"},
		{"order_amount", fmt.Sprintf("%.0f", float64(params.Amount))},
		{"currency", "VND"},
		{"order_invoice_number", invoiceNumber},
		{"order_description", params.Description},
	}

	if params.ReturnURL != "" {
		fields = append(fields, keyValue{"success_url", params.ReturnURL})
	} else if c.data.SuccessURL != "" {
		fields = append(fields, keyValue{"success_url", c.data.SuccessURL})
	}
	if c.data.ErrorURL != "" {
		fields = append(fields, keyValue{"error_url", c.data.ErrorURL})
	}
	if c.data.CancelURL != "" {
		fields = append(fields, keyValue{"cancel_url", c.data.CancelURL})
	}

	sig := signFields(fields, c.data.SecretKey)

	q := url.Values{}
	for _, kv := range fields {
		q.Set(kv.key, kv.value)
	}
	q.Set("signature", sig)

	return payment.ChargeResult{
		ProviderID:  invoiceNumber,
		RedirectURL: c.checkoutURL + "?" + q.Encode(),
		Status:      payment.StatusPending,
	}, nil
}

func (c *ClientImpl) Refund(ctx context.Context, params payment.RefundParams) (payment.RefundResult, error) {
	return payment.RefundResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Tokenize(ctx context.Context, params payment.TokenizeParams) (payment.TokenizeResult, error) {
	return payment.TokenizeResult{}, payment.ErrNotSupported
}

type ipnPayload struct {
	NotificationType string `json:"notification_type"`
	Order            struct {
		OrderStatus        string `json:"order_status"`
		OrderInvoiceNumber string `json:"order_invoice_number"`
	} `json:"order"`
	Transaction struct {
		TransactionStatus string `json:"transaction_status"`
	} `json:"transaction"`
}

func (c *ClientImpl) WireWebhooks(e *echo.Echo, deliver payment.NotificationHandler, registered map[string]struct{}) string {
	const key = "payment/sepay"
	if _, ok := registered[key]; ok {
		return key
	}
	e.POST("/api/v1/payment/webhook/sepay", func(ec echo.Context) error {
		if ec.Request().Header.Get("X-Secret-Key") != c.data.IPNSecretKey {
			slog.Error("sepay webhook: invalid secret key")
			return ec.JSON(http.StatusUnauthorized, map[string]bool{"success": false})
		}

		var payload ipnPayload
		if err := json.NewDecoder(ec.Request().Body).Decode(&payload); err != nil {
			slog.Error("sepay webhook: decode body", slog.Any("error", err))
			return ec.JSON(http.StatusBadRequest, map[string]bool{"success": false})
		}

		invoiceNumber := payload.Order.OrderInvoiceNumber
		if invoiceNumber == "" {
			slog.Error("sepay webhook: missing order_invoice_number")
			return ec.JSON(http.StatusBadRequest, map[string]bool{"success": false})
		}

		notification := payment.Notification{
			RefID:  invoiceNumber,
			Status: mapOrderStatus(payload.Order.OrderStatus),
		}

		if err := deliver(ec.Request().Context(), notification); err != nil {
			slog.Error("sepay webhook: deliver error", slog.Any("error", err))
		}

		return ec.JSON(http.StatusOK, map[string]bool{"success": true})
	})
	return key
}

func mapOrderStatus(status string) payment.Status {
	switch status {
	case "CAPTURED":
		return payment.StatusSuccess
	case "CANCELLED", "DECLINED", "ERROR":
		return payment.StatusFailed
	case "EXPIRED":
		return payment.StatusExpired
	default:
		return payment.StatusPending
	}
}
