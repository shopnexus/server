// Package carrier makes and reads shipments at the carriers a seller can
// link, GHTK and GHN, with the seller's own account there. Fake stands in
// for both in development and tests.
package carrier

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Carrier is a carrier's code: ghtk, ghn, spx, jt or viettel_post. Only
// GHTK and GHN have a client, which each stack runs live, fake or off
// (CARRIER_GHTK, CARRIER_GHN).
type Carrier string

const (
	GHTK Carrier = "ghtk"
	GHN  Carrier = "ghn"
)

// HasClient tells whether c is a carrier with a client, whether or not
// this stack runs it.
func (c Carrier) HasClient() bool { return c == GHTK || c == GHN }

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
	// ErrUnavailable is a carrier this stack runs off: nothing makes or
	// reads shipments there.
	ErrUnavailable = errors.New("carrier: this stack runs the carrier off")
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
	// ErrPhoneUnknown is a phone number with no account at the carrier.
	ErrPhoneUnknown = errors.New("carrier: no account at the carrier has this phone")
	// ErrOTPInvalid is a wrong or expired code the carrier texted.
	ErrOTPInvalid = errors.New("carrier: the code is wrong or expired")
)

// RefusedError is ErrRefused with the carrier's own words as its Reason.
type RefusedError struct{ Reason string }

func (e RefusedError) Error() string { return ErrRefused.Error() + ": " + e.Reason }

func (RefusedError) Is(target error) bool { return target == ErrRefused }

// Credentials are what a seller types to link an account: GHTK's email and
// password, GHN's token and ShopId, or a phone at GHN and the code GHN
// texted to it.
type Credentials struct {
	Email, Password string
	Token, ShopID   string
	Phone, OTP      string
}

// Account is a linked account as the clients use it. An empty Token is a
// GHN shop ShopNexus joined as staff, called with ShopNexus's token.
type Account struct {
	Token    string
	ShopCode string
}

// Linked is what linking returns: the shop code to show and the token to
// keep, empty for a shop ShopNexus joined as staff.
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
	// Note is a line for the shipper, such as when the recipient takes
	// parcels; empty for none.
	Note string
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
// ShopNexus masks it, where it goes, the COD amount, the fee, nil while the
// carrier tells none, the step it is at, empty for a status that maps to
// none, and the events the carrier keeps of it, oldest first.
type Parcel struct {
	RecipientName string
	District      string
	COD           int64
	Fee           *int64
	Step          Step
	Events        []Event
}

// Status is the order's status at the parcel's step: pending_pickup for a
// step that moves nothing, which a new shipment reads as.
func (p Parcel) Status() Status {
	if s, ok := p.Step.Status(); ok {
		return s
	}
	return StatusPendingPickup
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

// Texter is a carrier that links a seller's account by a code it texts to
// the seller's phone.
type Texter interface {
	// SendOTP texts the code; ErrPhoneUnknown when no account has phone.
	SendOTP(ctx context.Context, phone string) error
}

// Reporter is a carrier that posts webhooks. Report reads one: the
// parcel's code and the events it tells, none for one that tells nothing
// kept. contentType is the request's; now stands in for a time it lacks.
type Reporter interface {
	Report(contentType string, body []byte, now time.Time) (code string, evs []Event, err error)
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
