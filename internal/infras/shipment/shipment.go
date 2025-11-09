package shipment

import (
	"context"
	"time"

	"shopnexus-remastered/internal/db"
	commonmodel "shopnexus-remastered/internal/module/common/model"
)

// CreateParams represents the data required to create a shipment.
type CreateParams struct {
	FromAddress string `validate:"required,min=5,max=500"`
	ToAddress   string `validate:"required,min=5,max=500"`

	Package PackageDetails `validate:"required,dive"`
}

type PackageDetails struct {
	WeightGrams int32 `json:"weight_grams" validate:"required,min=1"`
	LengthCM    int32 `json:"length_cm" validate:"required,min=1"`
	WidthCM     int32 `json:"width_cm" validate:"required,min=1"`
	HeightCM    int32 `json:"height_cm" validate:"required,min=1"`
}

// ShippingOrder represents a created shipment with tracking info.
type ShippingOrder struct {
	ID       string // third-party tracking id
	Service  string
	LabelURL string
	ETA      time.Time               // e.g. ISO8601 format
	Costs    commonmodel.Concurrency // in USDT
}

type QuoteResult struct {
	ETA   time.Time               // e.g. ISO8601 format
	Costs commonmodel.Concurrency // in USDT
}

// TrackResult represents the real-time status of a shipment.
type TrackResult struct {
	ID        string                 // third-party tracking id
	Status    db.OrderShipmentStatus // e.g. "in_transit", "delivered"
	UpdatedAt string                 // ISO8601 timestamp
	Location  string                 // optional
}

type Client interface {
	// Config returns the option config for this shipment client.
	Config() commonmodel.OptionConfig

	// Quote calculates estimated cost & ETD without creating a shipment.
	Quote(ctx context.Context, params CreateParams) (QuoteResult, error)

	// Create books a shipment and returns label + tracking info.
	Create(ctx context.Context, params CreateParams) (ShippingOrder, error)

	// Track returns the current status for a given tracking id.
	Track(ctx context.Context, id string) (TrackResult, error)

	// Cancel attempts to cancel a shipment by its id.
	Cancel(ctx context.Context, id string) error
}
