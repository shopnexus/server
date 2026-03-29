package cod

import (
	"context"

	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/labstack/echo/v4"
)

// Type guard
var _ payment.Client = (*ClientImpl)(nil)

const (
	MethodCOD sharedmodel.OptionMethod = "cod"
)

type ClientImpl struct {
	config sharedmodel.OptionConfig
}

func NewClient() *ClientImpl {
	return &ClientImpl{
		config: sharedmodel.OptionConfig{
			ID:       "system-cod",
			Provider: "system",
			Method:   MethodCOD,
			Name:     "System - COD",
		},
	}
}

func (c *ClientImpl) Config() sharedmodel.OptionConfig {
	return c.config
}

func (c *ClientImpl) Create(ctx context.Context, params payment.CreateParams) (payment.CreateResult, error) {
	return payment.CreateResult{}, nil
}

func (c *ClientImpl) Get(ctx context.Context, providerID string) (payment.PaymentInfo, error) {
	return payment.PaymentInfo{
		ProviderID: providerID,
		Status:     payment.StatusPending,
	}, nil
}

func (c *ClientImpl) OnResult(fn payment.ResultHandler) {
	// COD has no webhooks
}

func (c *ClientImpl) InitializeWebhook(e *echo.Echo) {
	// COD has no webhooks
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
