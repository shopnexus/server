package transport

import (
	"context"
	"encoding/json"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// WebhookResult contains verified webhook/tracking update from transport provider.
// Status is a string matching orderdb.OrderTransportStatus values to avoid circular imports.
type WebhookResult struct {
	TransportID string         // Provider's internal ID mapping to order.transport tracking data
	Status      string         // Maps to orderdb.OrderTransportStatus (e.g. "InTransit", "Delivered")
	Data        map[string]any // Raw provider event data (for JSONB storage)
}

// ResultHandler is called after webhook verification with a parsed result.
type ResultHandler func(ctx context.Context, result WebhookResult) error

type Client interface {
	Config() sharedmodel.Option
	Quote(ctx context.Context, params QuoteParams) (QuoteResult, error)
	Create(ctx context.Context, params CreateParams) (Transport, error)
	Track(ctx context.Context, id string) (TrackResult, error)
	Cancel(ctx context.Context, id string) error

	// WireWebhooks mounts the provider's webhook route on Echo, delivering
	// verified events to `deliver`, and returns an idempotency key identifying
	// that route. If the key already appears in `registered`, the call is a
	// no-op (returns the key without mounting). An empty key means the provider
	// has no webhooks.
	WireWebhooks(e *echo.Echo, deliver ResultHandler, registered map[string]struct{}) string
}

type QuoteParams struct {
	Items       []ItemMetadata
	FromAddress string
	ToAddress   string
}

type ItemMetadata struct {
	SkuID          uuid.UUID
	Quantity       int64
	PackageDetails json.RawMessage
}

type CreateParams struct {
	Items       []ItemMetadata
	FromAddress string
	ToAddress   string
	Option      string
}

type QuoteResult struct {
	Cost int64
	Data json.RawMessage
}

type Transport struct {
	ID     uuid.UUID
	Option string
	Cost   int64
	Data   json.RawMessage
}

type TrackResult struct {
	Status string
	Data   json.RawMessage
}
