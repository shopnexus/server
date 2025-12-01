package cod

import (
	"context"

	"shopnexus-remastered/internal/infras/payment"
	commonmodel "shopnexus-remastered/internal/shared/model"
)

// Type guard
var _ payment.Client = (*ClientImpl)(nil)

const (
	// MethodCOD represents the Cash on Delivery method.
	MethodCOD commonmodel.OptionMethod = "cod"
)

// ClientImpl default COD (Cash on Delivery) client implementation
type ClientImpl struct {
	config commonmodel.OptionConfig
}

func NewClient() *ClientImpl {
	return &ClientImpl{
		config: commonmodel.OptionConfig{
			ID:       "system-cod",
			Provider: "system",
			Method:   MethodCOD,
			Name:     "System - COD",
		},
	}
}

func (c *ClientImpl) Config() commonmodel.OptionConfig {
	return c.config
}

func (c *ClientImpl) CreateOrder(ctx context.Context, params payment.CreateOrderParams) (payment.CreateOrderResult, error) {
	// For COD, we don't need a redirect URL.
	return payment.CreateOrderResult{
		RedirectURL: "",
	}, nil
}

func (c *ClientImpl) VerifyPayment(ctx context.Context, data map[string]any) (payment.VerifyResult, error) {
	// For COD, we assume payment is verified upon delivery.
	refID, ok := data["ref_id"].(string)
	if !ok {
		return payment.VerifyResult{}, nil // or return an error
	}
	return payment.VerifyResult{
		RefID: refID,
	}, nil
}
