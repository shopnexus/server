package shipment

import (
	"context"
	"shopnexus-remastered/internal/db"
	"time"
)

// CreateShipmentParams represents the data required to create a shipment.
type CreateShipmentParams struct {
	OrderID     string
	FromAddress string
	ToAddress   string
	WeightGrams int64
	Dimensions  Dimensions // Optional
	Service     string     // e.g. "express", "standard"
}

type Dimensions struct {
	LengthCM int
	WidthCM  int
	HeightCM int
}

// Shipment represents a created shipment with tracking info.
type Shipment struct {
	ID         string
	LabelURL   string
	TrackingID string
	Service    string
	ETA        time.Time // e.g. ISO8601 format
	CostCents  int64
}

// TrackResult represents the real-time status of a shipment.
type TrackResult struct {
	TrackingID string
	Status     db.OrderShipmentStatus // e.g. "in_transit", "delivered"
	UpdatedAt  string                 // ISO8601 timestamp
	Location   string                 // optional
}

type Client interface {
	// Quote calculates estimated cost & ETD without creating a shipment.
	Quote(ctx context.Context, params CreateShipmentParams) (Shipment, error)

	// CreateShipment books a shipment and returns label + tracking info.
	CreateShipment(ctx context.Context, params CreateShipmentParams) (Shipment, error)

	// Track returns the current status for a given tracking ID.
	Track(ctx context.Context, trackingID string) (TrackResult, error)
}
