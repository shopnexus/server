package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

const (
	// MaxOpenOrders is how many orders a buyer may have pending or shipped
	// at once.
	MaxOpenOrders = 3
	// MaxWeight is the heaviest parcel a linked shipment takes, in grams.
	MaxWeight       = 20_000
	maxTrackingCode = 50
)

type OrderStatus string

const (
	OrderPending   OrderStatus = "pending"
	OrderShipped   OrderStatus = "shipped"
	OrderCompleted OrderStatus = "completed"
	OrderFailed    OrderStatus = "failed"
	OrderCancelled OrderStatus = "cancelled"
)

// CancelReason is why an order was cancelled.
type CancelReason string

const (
	CancelByBuyer  CancelReason = "buyer"
	CancelBySeller CancelReason = "seller"
	// CancelSellerTimeout is a pending order whose seller made no shipment
	// within Waits.Seller.
	CancelSellerTimeout CancelReason = "seller_timeout"
	// CancelPickupTimeout is a shipped order whose parcel the carrier had
	// not picked up Waits.Pickup after the shipment.
	CancelPickupTimeout CancelReason = "pickup_timeout"
	// CancelShippedToAnother is a pending order whose listing the seller
	// shipped to another buyer.
	CancelShippedToAnother CancelReason = "shipped_to_another"
	// CancelListingClosed is a pending order whose listing was hidden, sold
	// off the app or deleted.
	CancelListingClosed CancelReason = "listing_closed"
)

// Carrier is a carrier ShopNexus reads shipments from.
type Carrier string

const (
	CarrierGHTK        Carrier = "ghtk"
	CarrierGHN         Carrier = "ghn"
	CarrierSPX         Carrier = "spx"
	CarrierJT          Carrier = "jt"
	CarrierViettelPost Carrier = "viettel_post"
)

var carriers = []Carrier{CarrierGHTK, CarrierGHN, CarrierSPX, CarrierJT, CarrierViettelPost}

// ShipmentMode says who made the shipment: the server on the seller's
// linked GHTK account, or the seller on a carrier's own app.
type ShipmentMode string

const (
	ModeLinked ShipmentMode = "linked"
	ModeManual ShipmentMode = "manual"
)

// ShipmentStatus is where the parcel is, as its carrier reports it.
type ShipmentStatus string

const (
	ShipmentPendingPickup ShipmentStatus = "pending_pickup"
	ShipmentInTransit     ShipmentStatus = "in_transit"
	ShipmentDelivered     ShipmentStatus = "delivered"
	ShipmentFailed        ShipmentStatus = "failed"
)

// Shipment is the parcel of a shipped order. Fee is the carrier's, nil
// while it has none; DeliveredAt is set while the status is delivered.
type Shipment struct {
	Mode         ShipmentMode   `json:"mode"`
	Carrier      Carrier        `json:"carrier"`
	TrackingCode string         `json:"tracking_code"`
	Fee          *int64         `json:"fee,omitempty"`
	Status       ShipmentStatus `json:"status"`
	DeliveredAt  *time.Time     `json:"delivered_at,omitempty"`
}

// LinkedService is the GHTK service a linked shipment goes by.
type LinkedService string

const (
	ServiceStandard LinkedService = "standard"
	ServiceExpress  LinkedService = "express"
)

// Pickup is where GHTK takes a linked shipment's parcel: from the seller's
// home, or at a post office the seller brings it to.
type Pickup string

const (
	PickupHome       Pickup = "home"
	PickupPostOffice Pickup = "post_office"
)

// LinkedDraft is what the seller picks for a linked shipment; the server
// makes the shipment at GHTK.
type LinkedDraft struct {
	Service     LinkedService `json:"service"`
	WeightGrams int           `json:"weight_grams"`
	Pickup      Pickup        `json:"pickup"`
}

// Waits are how long the system waits before it moves an order on.
type Waits struct {
	// Seller is how long a pending order waits for its shipment.
	Seller time.Duration
	// Pickup is how long a shipment waits for the carrier's pickup.
	Pickup time.Duration
	// Complete is how long a delivered parcel waits for the buyer's word.
	Complete time.Duration
}

// OrderWaits are the waits of every order: 3 days each.
var OrderWaits = Waits{Seller: 72 * time.Hour, Pickup: 72 * time.Hour, Complete: 72 * time.Hour}

// Recipient is where the parcel goes. Phone is in the +84 form.
type Recipient struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Street      string `json:"street"`
	CommuneCode string `json:"commune_code"`
}

// Order is a buyer's request for cash on delivery at the agreed price, and
// what became of it. Code is set by the store.
type Order struct {
	ID           uuid.UUID    `json:"id"`
	Code         string       `json:"code"`
	Status       OrderStatus  `json:"status"`
	Price        int64        `json:"price"`
	Recipient    Recipient    `json:"recipient"`
	Shipment     *Shipment    `json:"shipment,omitempty"`
	CancelledBy  Party        `json:"cancelled_by,omitempty"`
	CancelReason CancelReason `json:"cancel_reason,omitempty"`
	RequestedAt  time.Time    `json:"requested_at"`
	ShippedAt    *time.Time   `json:"shipped_at,omitempty"`
	ClosedAt     *time.Time   `json:"closed_at,omitempty"`
}

// Tracking checks a carrier and a tracking code as the seller typed them:
// a carrier ShopNexus reads, and 1 to 50 characters once trimmed. It
// returns the code trimmed.
func Tracking(carrier, code string) (Carrier, string, error) {
	e := ErrInvalidShipment
	c, code := Carrier(carrier), strings.TrimSpace(code)
	if !slices.Contains(carriers, c) {
		e = e.WithField("carrier", "ghtk, ghn, spx, jt or viettel_post")
	}
	if n := utf8.RuneCountInString(code); n < 1 || n > maxTrackingCode {
		e = e.WithField("tracking_code", "1 to 50 characters")
	}
	if len(e.Fields) > 0 {
		return "", "", e
	}
	return c, code, nil
}

// NewLinkedDraft checks what the seller picked for a linked shipment.
func NewLinkedDraft(service string, weightGrams int, pickup string) (LinkedDraft, error) {
	e := ErrInvalidShipment
	s, p := LinkedService(service), Pickup(pickup)
	if s != ServiceStandard && s != ServiceExpress {
		e = e.WithField("service", "standard or express")
	}
	if weightGrams < 1 || weightGrams > MaxWeight {
		e = e.WithField("weight_grams", "1 to 20,000 grams")
	}
	if p != PickupHome && p != PickupPostOffice {
		e = e.WithField("pickup", "home or post_office")
	}
	if len(e.Fields) > 0 {
		return LinkedDraft{}, e
	}
	return LinkedDraft{Service: s, WeightGrams: weightGrams, Pickup: p}, nil
}

// ParseShipmentStatus reads a status a carrier, or the dev endpoint,
// reports.
func ParseShipmentStatus(s string) (ShipmentStatus, error) {
	st := ShipmentStatus(s)
	switch st {
	case ShipmentPendingPickup, ShipmentInTransit, ShipmentDelivered, ShipmentFailed:
		return st, nil
	}
	return "", ErrInvalidStatus
}

// Open reports whether the order is pending or shipped.
func (o Order) Open() bool { return o.Status == OrderPending || o.Status == OrderShipped }

// RecipientFor is the recipient as viewer may read it: the seller of a
// cancelled order keeps only the name and the commune.
func (o Order) RecipientFor(viewer Party) Recipient {
	r := o.Recipient
	if viewer == Seller && o.Status == OrderCancelled {
		r.Phone, r.Street = "", ""
	}
	return r
}
