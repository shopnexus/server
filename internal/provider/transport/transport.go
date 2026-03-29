package transport

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	sharedmodel "shopnexus-server/internal/shared/model"
)

type Client interface {
	Config() sharedmodel.OptionConfig
	Quote(ctx context.Context, params QuoteParams) (QuoteResult, error)
	Create(ctx context.Context, params CreateParams) (Transport, error)
	Track(ctx context.Context, id string) (TrackResult, error)
	Cancel(ctx context.Context, id string) error
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
