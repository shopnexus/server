package payment

import (
	"context"

	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/labstack/echo/v4"
)

// Status represents the normalized payment status across all providers.
type Status string

const (
	StatusPending Status = "pending"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusExpired Status = "expired"
)

// CreateParams contains the parameters needed to create a payment with any provider.
type CreateParams struct {
	RefID       int64                   // internal payment record ID
	Amount      sharedmodel.Concurrency // payment amount
	Description string                  // human-readable description
	ReturnURL   string                  // where to redirect after payment (provider may override)
}

// CreateResult contains the result of creating a payment.
type CreateResult struct {
	ProviderID  string // provider-side transaction/order ID (for tracking)
	RedirectURL string // redirect URL for online payments, empty for COD/offline
}

// PaymentInfo contains normalized payment information from the provider.
type PaymentInfo struct {
	ProviderID string
	RefID      string // maps back to our internal reference
	Status     Status
	Amount     int64
}

// WebhookResult contains the result of verifying a webhook/IPN callback.
type WebhookResult struct {
	RefID  string // maps back to our internal payment reference
	Status Status // the payment status reported by the provider
}

// ResultHandler is a callback invoked when a webhook is verified.
type ResultHandler func(ctx context.Context, result WebhookResult) error

// Client is the interface that all payment providers must implement.
type Client interface {
	Config() sharedmodel.OptionConfig
	Create(ctx context.Context, params CreateParams) (CreateResult, error)
	Get(ctx context.Context, providerID string) (PaymentInfo, error)

	// OnResult registers a handler that is called when a webhook is verified.
	// Multiple handlers can be registered; all are called (fan-out).
	OnResult(fn ResultHandler)

	// InitializeWebhook registers the provider's webhook route on Echo.
	// Must be called after OnResult handlers are registered.
	InitializeWebhook(e *echo.Echo)
}
