// Package carrier makes and reads shipments at the carriers a seller can
// link, GHTK and GHN, with the seller's own account there. Fake stands in
// for both in development and tests.
package carrier

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Carrier is a carrier's code: ghtk, ghn, spx, jt or viettel_post. Only
// GHTK and GHN have a client.
type Carrier string

const (
	GHTK Carrier = "ghtk"
	GHN  Carrier = "ghn"
)

// Status is where a parcel is, in ShopNexus's words.
type Status string

const (
	StatusPendingPickup Status = "pending_pickup"
	StatusInTransit     Status = "in_transit"
	StatusDelivered     Status = "delivered"
	StatusFailed        Status = "failed"
)

var (
	// ErrNotFound is a tracking code the seller's account at the carrier
	// does not know.
	ErrNotFound = errors.New("carrier: no such shipment in this account")
	// ErrUnavailable is a stack without a carrier, CARRIER=none: nothing
	// makes or reads shipments there.
	ErrUnavailable = errors.New("carrier: no carrier in this stack")
	// ErrLoginFailed is an account the carrier refused to link.
	ErrLoginFailed = errors.New("carrier: the carrier refused the account")
	// ErrNotCancellable is a shipment the carrier has already picked up.
	ErrNotCancellable = errors.New("carrier: the carrier has picked the parcel up")
	// ErrRefused is a request the carrier refused for a reason of its own
	// (a phone number, a weight, an address it will not serve); the error
	// carries its words. Trying the same request again does not help.
	ErrRefused = errors.New("carrier: the carrier refused the request")
	// ErrAddressUnknown is an address the carrier does not deliver to or
	// does not know.
	ErrAddressUnknown = errors.New("carrier: the carrier does not know the address")
)

// Credentials are what a seller types to link an account: GHTK's email and
// password, or GHN's token and ShopId.
type Credentials struct {
	Email, Password string
	Token, ShopID   string
}

// Account is a linked account as the clients use it.
type Account struct {
	Token    string
	ShopCode string
}

// Linked is what linking returns: the shop code to show and the token to
// keep.
type Linked struct {
	ShopCode string
	Token    string
}

// PickupPoint is a pickup point the seller registered at the carrier.
type PickupPoint struct {
	ID, Name, Phone, Address string
}

// Place is an address by names, as the carriers take them: the street,
// the commune (phường or xã) and the province.
type Place struct {
	Name, Phone, Street, Commune, Province string
}

// Pickup is where the carrier takes the parcel: one of the seller's pickup
// points at the carrier, a place, or neither for the carrier's default.
type Pickup struct {
	PointID string
	Place   *Place
}

// CreateInput is a shipment to make on the seller's account. OrderCode is
// ShopNexus's order code; a retry with the same code makes no second
// shipment.
type CreateInput struct {
	OrderCode string
	Recipient Place
	Pickup    Pickup
	Item      string
	// COD is what the carrier collects at the door, in đồng.
	COD         int64
	WeightGrams int
	Service     string // standard or express
	Option      string // home or post_office
}

// QuoteInput is a shipment to price.
type QuoteInput struct {
	Recipient   Place
	Pickup      Pickup
	WeightGrams int
	Service     string
	// Value is the parcel's declared value, in đồng.
	Value int64
}

// Created is a new shipment's tracking code and fee, in đồng.
type Created struct {
	TrackingCode string `json:"tracking_code"`
	Fee          int64  `json:"fee"`
}

// Parcel is what a carrier tells of a shipment: the recipient's name as
// ShopNexus masks it, where it goes, the COD amount, the status, and the
// fee, nil while the carrier tells none. Moved is false for a status that
// moves nothing (delays, notices, a cancel), which reads as pending_pickup
// for a new shipment and must not move one already on its way.
type Parcel struct {
	RecipientName string
	District      string
	COD           int64
	Status        Status
	Moved         bool
	Fee           *int64
}

// Client is one carrier, called with a seller's account.
type Client interface {
	// Link checks the seller's credentials at the carrier and returns the
	// token to keep.
	Link(ctx context.Context, c Credentials) (Linked, error)
	// Pickups lists the seller's pickup points at the carrier.
	Pickups(ctx context.Context, a Account) ([]PickupPoint, error)
	// Quote prices a shipment, in đồng.
	Quote(ctx context.Context, a Account, in QuoteInput) (int64, error)
	// Create makes a shipment on the seller's account.
	Create(ctx context.Context, a Account, in CreateInput) (Created, error)
	// Lookup reads a shipment by code; ErrNotFound when the seller's
	// account does not know it.
	Lookup(ctx context.Context, a Account, code string) (Parcel, error)
	// Cancel calls a shipment off; ErrNotCancellable once the carrier has
	// the parcel. A second call changes nothing.
	Cancel(ctx context.Context, a Account, code string) error
}

// mask keeps each word's first letter: "Vân Nguyễn" → "V** N*****".
func mask(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		r, size := utf8.DecodeRuneInString(w)
		words[i] = string(r) + strings.Repeat("*", utf8.RuneCountInString(w[size:]))
	}
	return strings.Join(words, " ")
}
