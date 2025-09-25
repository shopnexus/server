package shipment

import "context"

// ShipmentRequest represents the data required to create a shipment.
type ShipmentRequest struct {
	OrderID     string
	FromAddress Address
	ToAddress   Address
	WeightGrams int64
	Dimensions  Dimensions // Optional
	Service     string     // e.g. "express", "standard"
}

type Address struct {
	Name    string
	Street  string
	City    string
	State   string
	Zip     string
	Country string
}

type Dimensions struct {
	LengthCM int
	WidthCM  int
	HeightCM int
}

// Shipment represents a created shipment with tracking info.
type Shipment struct {
	ID           string
	LabelURL     string
	TrackingID   string
	Service      string
	EstimatedETD string // e.g. ISO8601 format
	CostCents    int64
}

// ShipmentStatus represents the real-time status of a shipment.
type ShipmentStatus struct {
	TrackingID string
	Status     string // e.g. "in_transit", "delivered"
	UpdatedAt  string // ISO8601 timestamp
	Location   string // optional
}

type Client interface {
	// Quote calculates estimated cost & ETD without creating a shipment.
	Quote(ctx context.Context, req ShipmentRequest) (Shipment, error)

	// CreateShipment books a shipment and returns label + tracking info.
	CreateShipment(ctx context.Context, req ShipmentRequest) (Shipment, error)

	// Track returns the current status for a given tracking ID.
	Track(ctx context.Context, trackingID string) (ShipmentStatus, error)
}
