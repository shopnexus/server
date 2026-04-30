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

type Data struct {
	Processor string `json:"processor"`
	SecretKey string `json:"secret_key"`
	PublicKey string `json:"public_key"`
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
	// TODO: real processor (Stripe, PayOS, ...)
	return payment.ChargeResult{}, fmt.Errorf("card provider %q: charge not implemented", c.data.Processor)
}

func (c *ClientImpl) Refund(ctx context.Context, params payment.RefundParams) (payment.RefundResult, error) {
	// TODO: real processor
	return payment.RefundResult{}, fmt.Errorf("card provider %q: refund not implemented", c.data.Processor)
}

func (c *ClientImpl) Tokenize(ctx context.Context, params payment.TokenizeParams) (payment.TokenizeResult, error) {
	return payment.TokenizeResult{
		ClientConfig: json.RawMessage(fmt.Sprintf(`{"provider":"%s","public_key":"%s"}`, c.data.Processor, c.data.PublicKey)),
	}, nil
}

// no-op: card charges are synchronous, no webhooks to mount.
func (c *ClientImpl) WireWebhooks(e *echo.Echo, deliver payment.NotificationHandler, registered map[string]struct{}) string {
	return ""
}
