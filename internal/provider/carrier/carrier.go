// Package carrier makes and reads shipments at the carriers ShopNexus can
// read: GHTK, GHN, SPX, J&T and Viettel Post. Fake stands in for all of
// them until the real clients come.
package carrier

import (
	"context"
	"errors"
	"uuid"
)

// Carrier is a carrier's code: ghtk, ghn, spx, jt or viettel_post.
type Carrier string

// Status is where a parcel is, in ShopNexus's words.
type Status string

const (
	StatusPendingPickup Status = "pending_pickup"
	StatusInTransit     Status = "in_transit"
	StatusDelivered     Status = "delivered"
	StatusFailed        Status = "failed"
)

var (
	// ErrNotFound is a tracking code the carrier does not know.
	ErrNotFound = errors.New("carrier: no such shipment")
	// ErrUnavailable is a stack without a carrier, CARRIER=none: nothing
	// makes or reads shipments there.
	ErrUnavailable = errors.New("carrier: no carrier in this stack")
)

// Recipient is where a parcel goes. Phone is in the +84 form.
type Recipient struct {
	Name        string
	Phone       string
	Street      string
	CommuneCode string
}

// CreateInput is a shipment to make on the seller's linked GHTK account.
// OrderID is the order's ID, so a retried Create makes no second shipment.
type CreateInput struct {
	OrderID   uuid.UUID
	Recipient Recipient
	// COD is what the carrier collects at the door, in đồng.
	COD         int64
	WeightGrams int
	Service     string // standard or express
	Pickup      string // home or post_office
}

// Created is a new shipment's tracking code and fee, in đồng.
type Created struct {
	TrackingCode string `json:"tracking_code"`
	Fee          int64  `json:"fee"`
}

// Parcel is what a carrier tells of a shipment: the recipient's name as it
// masks it, the district, the COD amount, the status, and the fee, nil
// while it has none.
type Parcel struct {
	RecipientName string
	District      string
	COD           int64
	Status        Status
	Fee           *int64
}

// Client makes, reads and calls off shipments.
type Client interface {
	// Create makes a shipment on the seller's linked account: its tracking
	// code and fee.
	Create(ctx context.Context, in CreateInput) (Created, error)
	// Lookup reads a shipment by carrier and code; ErrNotFound when the
	// carrier does not know it.
	Lookup(ctx context.Context, c Carrier, code string) (Parcel, error)
	// Cancel calls off a shipment the carrier has not picked up; a second
	// call changes nothing.
	Cancel(ctx context.Context, c Carrier, code string) error
}
