package card

import (
	"context"
	"encoding/json"
	"fmt"

	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/labstack/echo/v4"
)

var _ payment.Client = (*ClientImpl)(nil)

type ClientImpl struct {
	config    sharedmodel.OptionConfig
	provider  string
	secretKey string
	publicKey string
	handlers  []payment.ResultHandler
}

type ClientOptions struct {
	Provider  string
	SecretKey string
	PublicKey string
}

func NewClient(cfg ClientOptions) *ClientImpl {
	return &ClientImpl{
		config: sharedmodel.OptionConfig{
			ID:       "card_" + cfg.Provider,
			Provider: cfg.Provider,
			Name:     "Card Payment (" + cfg.Provider + ")",
		},
		provider:  cfg.Provider,
		secretKey: cfg.SecretKey,
		publicKey: cfg.PublicKey,
	}
}

func (c *ClientImpl) Config() sharedmodel.OptionConfig {
	return c.config
}

func (c *ClientImpl) Create(ctx context.Context, params payment.CreateParams) (payment.CreateResult, error) {
	return payment.CreateResult{}, payment.ErrNotSupported
}

func (c *ClientImpl) Get(ctx context.Context, providerID string) (payment.PaymentInfo, error) {
	return payment.PaymentInfo{}, payment.ErrNotSupported
}

func (c *ClientImpl) OnResult(fn payment.ResultHandler) {
	c.handlers = append(c.handlers, fn)
}

func (c *ClientImpl) InitializeWebhook(e *echo.Echo) {
	// Card charges are synchronous — webhook is optional backup
}

func (c *ClientImpl) Charge(ctx context.Context, params payment.ChargeParams) (payment.ChargeResult, error) {
	// TODO: implement with real processor (Stripe, PayOS, etc.)
	return payment.ChargeResult{}, fmt.Errorf("card provider %q: charge not implemented", c.provider)
}

func (c *ClientImpl) Refund(ctx context.Context, params payment.RefundParams) (payment.RefundResult, error) {
	// TODO: implement with real processor
	return payment.RefundResult{}, fmt.Errorf("card provider %q: refund not implemented", c.provider)
}

func (c *ClientImpl) Tokenize(ctx context.Context, params payment.TokenizeParams) (payment.TokenizeResult, error) {
	// Return public key so FE can render the processor's JS SDK
	return payment.TokenizeResult{
		ClientConfig: json.RawMessage(fmt.Sprintf(`{"provider":"%s","public_key":"%s"}`, c.provider, c.publicKey)),
	}, nil
}
