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

	sandboxAPIURL = "https://pgapi-sandbox.sepay.vn"
	prodAPIURL    = "https://pgapi.sepay.vn"
)

type ClientImpl struct {
	config sharedmodel.OptionConfig

	merchantID   string
	secretKey    string
	ipnSecretKey string
	checkoutURL  string
	apiURL      string
	successURL  string
	errorURL    string
	cancelURL   string

	handlers []payment.ResultHandler
}

type ClientOptions struct {
	MerchantID   string
	SecretKey    string // checkout signing + API Basic Auth
	IPNSecretKey string // X-Secret-Key webhook verification
	SuccessURL   string
	ErrorURL   string
	CancelURL  string
	Sandbox    bool
}

func NewClient(cfg ClientOptions) *ClientImpl {
	checkoutURL := prodCheckoutURL
	apiURL := prodAPIURL
	if cfg.Sandbox {
		checkoutURL = sandboxCheckoutURL
		apiURL = sandboxAPIURL
	}

	return &ClientImpl{
		config: sharedmodel.OptionConfig{
			ID:       "sepay_bank_transfer",
			Provider: "sepay",
			Name:     "SePay - Bank Transfer",
		},
		merchantID:   cfg.MerchantID,
		secretKey:    cfg.SecretKey,
		ipnSecretKey: cfg.IPNSecretKey,
		checkoutURL:  checkoutURL,
		apiURL:      apiURL,
		successURL:  cfg.SuccessURL,
		errorURL:    cfg.ErrorURL,
		cancelURL:   cfg.CancelURL,
	}
}

func (c *ClientImpl) Config() sharedmodel.OptionConfig {
	return c.config
}

func (c *ClientImpl) Create(ctx context.Context, params payment.CreateParams) (payment.CreateResult, error) {
	invoiceNumber := fmt.Sprintf("%d", params.RefID)

	// Build form fields in SePay's required order for signature
	fields := []keyValue{
		{"merchant", c.merchantID},
		{"operation", "PURCHASE"},
		{"payment_method", "BANK_TRANSFER"},
		{"order_amount", fmt.Sprintf("%.0f", params.Amount.Float64())},
		{"currency", "VND"},
		{"order_invoice_number", invoiceNumber},
		{"order_description", params.Description},
	}

	if params.ReturnURL != "" {
		fields = append(fields, keyValue{"success_url", params.ReturnURL})
	} else if c.successURL != "" {
		fields = append(fields, keyValue{"success_url", c.successURL})
	}
	if c.errorURL != "" {
		fields = append(fields, keyValue{"error_url", c.errorURL})
	}
	if c.cancelURL != "" {
		fields = append(fields, keyValue{"cancel_url", c.cancelURL})
	}

	sig := signFields(fields, c.secretKey)

	// Build the redirect URL with form params
	q := url.Values{}
	for _, kv := range fields {
		q.Set(kv.key, kv.value)
	}
	q.Set("signature", sig)

	redirectURL := c.checkoutURL + "?" + q.Encode()

	return payment.CreateResult{
		ProviderID:  invoiceNumber,
		RedirectURL: redirectURL,
	}, nil
}

func (c *ClientImpl) Get(ctx context.Context, providerID string) (payment.PaymentInfo, error) {
	reqURL := fmt.Sprintf("%s/orders/%s", c.apiURL, providerID)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return payment.PaymentInfo{}, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(c.merchantID, c.secretKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return payment.PaymentInfo{}, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return payment.PaymentInfo{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Data struct {
			OrderStatus        string `json:"order_status"`
			OrderInvoiceNumber string `json:"order_invoice_number"`
			OrderAmount        string `json:"order_amount"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return payment.PaymentInfo{}, fmt.Errorf("decode response: %w", err)
	}

	return payment.PaymentInfo{
		ProviderID: body.Data.OrderInvoiceNumber,
		RefID:      body.Data.OrderInvoiceNumber,
		Status:     mapOrderStatus(body.Data.OrderStatus),
	}, nil
}

func (c *ClientImpl) OnResult(fn payment.ResultHandler) {
	c.handlers = append(c.handlers, fn)
}

// ipnPayload represents the SePay Payment Gateway IPN webhook JSON body.
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
	e.POST("/api/v1/payment/webhook/sepay", func(ec echo.Context) error {
		// Verify X-Secret-Key header (SePay PG IPN secret)
		if ec.Request().Header.Get("X-Secret-Key") != c.ipnSecretKey {
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

		result := payment.WebhookResult{
			RefID:  invoiceNumber,
			Status: mapOrderStatus(payload.Order.OrderStatus),
		}

		ctx := ec.Request().Context()
		for _, fn := range c.handlers {
			if err := fn(ctx, result); err != nil {
				slog.Error("sepay webhook: handler error", slog.Any("error", err))
			}
		}

		return ec.JSON(http.StatusOK, map[string]bool{"success": true})
	})
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
