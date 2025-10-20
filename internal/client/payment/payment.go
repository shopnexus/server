package payment

import (
	"context"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
)

type CreateOrderParams struct {
	RefID  int64
	Amount sharedmodel.Concurrency
	Info   string
}

type CreateOrderResult struct {
	RedirectURL string
}

type VerifyResult struct {
	RefID int64
}

type Client interface {
	// Config returns the payment configuration.
	Config() sharedmodel.OptionConfig

	// CreateOrder creates a payment order and returns either:
	// - a redirect URL (for online payments),
	// - or an empty string + metadata (for COD, Bank Transfer, etc.)
	CreateOrder(ctx context.Context, params CreateOrderParams) (CreateOrderResult, error)

	// VerifyPayment verifies webhook/IPN data and returns a normalized payment reference.
	VerifyPayment(ctx context.Context, data map[string]any) (VerifyResult, error)

	// GetPaymentStatus checks the current status (useful for async providers or retries).
	//GetPaymentStatus(ctx context.Context, referenceID string) (PaymentStatus, error)

	// Refund processes a refund/void if supported by the payment provider.
	//Refund(ctx context.Context, params RefundParams) (RefundResult, error)
}
