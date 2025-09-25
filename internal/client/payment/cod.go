package payment

import "context"

// CODClient default COD (Cash on Delivery) client implementation
type CODClient struct{}

func NewCODClient() *CODClient {
	return &CODClient{}
}

func (c *CODClient) CreateOrder(ctx context.Context, params CreateOrderParams) (CreateOrderResult, error) {
	// For COD, we don't need a redirect URL.
	return CreateOrderResult{
		RedirectURL: "",
	}, nil
}

func (c *CODClient) VerifyPayment(ctx context.Context, data map[string]any) (VerifyResult, error) {
	// For COD, we assume payment is verified upon delivery.
	refID, ok := data["ref_id"].(int64)
	if !ok {
		return VerifyResult{}, nil // or return an error
	}
	return VerifyResult{
		RefID: refID,
	}, nil
}
