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
	Config() sharedmodel.OptionConfig
	Quote(ctx context.Context, params QuoteParams) (QuoteResult, error)
	Create(ctx context.Context, params CreateParams) (Transport, error)
	Track(ctx context.Context, id string) (TrackResult, error)
	Cancel(ctx context.Context, id string) error

	// OnResult registers a callback for verified webhook events.
	// Multiple handlers can be registered; all are called (fan-out).
	OnResult(handler ResultHandler)

	// InitializeWebhook registers the provider's webhook route on Echo.
	InitializeWebhook(e *echo.Echo)
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
