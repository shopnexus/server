package domain

import (
	"slices"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

const (
	// MaxOpenOrders is how many orders a buyer may have pending or shipped
	// at once.
	MaxOpenOrders = 3
	// BuyerPeriod is how far back a buyer's figures reach.
	BuyerPeriod = 90 * 24 * time.Hour
	// MaxWeight is the heaviest parcel a linked shipment takes, in grams.
	MaxWeight = 20_000
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

// ShipmentMode says who made the shipment: the server on the seller's
// linked account at GHTK or GHN, or, on an order shipped before manual
// shipments were dropped, the seller on a carrier's own app.
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
	// ShipmentLost is a report only, never stored: the carrier cancelled
	// or lost the parcel. It fails an order picked up and moves no other.
	ShipmentLost ShipmentStatus = "lost"
)

// Shipment is the parcel of a shipped order. Fee is the carrier's, nil
// while it has none. PickedUpAt is when the carrier first reported it past
// pending_pickup, nil while it waits there; DeliveredAt is set while the
// status is delivered. StatusAt is when the carrier's step behind Status
// happened, nil before its first report; ShopCode is the carrier shop a
// linked shipment was made at. The API shows neither.
type Shipment struct {
	Mode         ShipmentMode   `json:"mode"`
	Carrier      Carrier        `json:"carrier"`
	TrackingCode string         `json:"tracking_code"`
	Fee          *int64         `json:"fee,omitempty"`
	Status       ShipmentStatus `json:"status"`
	PickedUpAt   *time.Time     `json:"picked_up_at,omitempty"`
	DeliveredAt  *time.Time     `json:"delivered_at,omitempty"`
	StatusAt     *time.Time     `json:"status_at,omitempty"`
	ShopCode     string         `json:"shop_code,omitempty"`
	// Pickup is where the carrier takes a linked shipment's parcel; nil
	// for one sent from the carrier's default pickup point, and for a
	// manual one.
	Pickup *ShipmentPickup `json:"pickup,omitempty"`
}

// ShipmentPickup is the pickup of a linked shipment as the order keeps it:
// one of the seller's pickup points at the carrier, or a copy of one of
// the seller's addresses, which a later edit of the address leaves alone.
type ShipmentPickup struct {
	PointID     string `json:"point_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Phone       string `json:"phone,omitempty"`
	Street      string `json:"street,omitempty"`
	CommuneCode string `json:"commune_code,omitempty"`
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

// ShipmentDraft is what the seller picks for a shipment; the server makes
// it on the seller's account at the carrier.
type ShipmentDraft struct {
	Carrier     Carrier       `json:"carrier"`
	Service     LinkedService `json:"service"`
	WeightGrams int           `json:"weight_grams"`
	Pickup      Pickup        `json:"pickup"`
	// From is where the carrier picks the parcel up; nil for the carrier's
	// default pickup point.
	From *ShipmentPickup `json:"from,omitempty"`
}

// Waits are how long the system waits before it moves an order on.
type Waits struct {
	// Seller is how long a pending order waits for its shipment.
	Seller time.Duration
	// Pickup is how long a shipment waits for the carrier's pickup.
	Pickup time.Duration
	// Complete is how long a delivered parcel waits for the buyer's word.
	Complete time.Duration
	// Transit is how long a picked-up parcel goes before the carrier is
	// asked whether it still moves.
	Transit time.Duration
}

// TransitWait is Waits.Transit for every order.
const TransitWait = 30 * 24 * time.Hour

// OrderWaits are the waits of every order: 3 days each, and TransitWait.
var OrderWaits = Waits{Seller: 72 * time.Hour, Pickup: 72 * time.Hour, Complete: 72 * time.Hour, Transit: TransitWait}

// AddressKind is what the recipient's place is, copied from the address
// book: an office (work) takes parcels in office hours.
type AddressKind string

const (
	AddressHome  AddressKind = "home"
	AddressWork  AddressKind = "work"
	AddressOther AddressKind = "other"
)

// Recipient is where the parcel goes, a copy of one of the buyer's
// addresses. Phone is in the +84 form.
type Recipient struct {
	Name        string      `json:"name"`
	Phone       string      `json:"phone"`
	Street      string      `json:"street"`
	CommuneCode string      `json:"commune_code"`
	Kind        AddressKind `json:"kind"`
}

// Order is a buyer's request for cash on delivery at the agreed price, and
// what became of it. Code is set by the store.
type Order struct {
	ID          uuid.UUID   `json:"id"`
	Code        string      `json:"code"`
	Status      OrderStatus `json:"status"`
	Price       int64       `json:"price"`
	Recipient   Recipient   `json:"recipient"`
	Shipment    *Shipment   `json:"shipment,omitempty"`
	CancelledBy Party       `json:"cancelled_by,omitempty"`
	// CompletedBy is the buyer, who confirmed receipt, or the system, after
	// the carrier reported the parcel delivered; empty until completed.
	CompletedBy  Party        `json:"completed_by,omitempty"`
	CancelReason CancelReason `json:"cancel_reason,omitempty"`
	RequestedAt  time.Time    `json:"requested_at"`
	ShippedAt    *time.Time   `json:"shipped_at,omitempty"`
	ClosedAt     *time.Time   `json:"closed_at,omitempty"`
}

// DueAt is when a timer moves the order on unless someone acts first:
// SellerTimeout for a pending order, PickupTimeout for a parcel waiting for
// pickup, AutoComplete for a delivered one. It is nil when no timer waits.
func (o Order) DueAt(w Waits) *time.Time {
	var at time.Time
	switch sh := o.Shipment; {
	case o.Status == OrderPending:
		at = o.RequestedAt.Add(w.Seller)
	case o.Status != OrderShipped || sh == nil:
		return nil
	case sh.Status == ShipmentPendingPickup && o.ShippedAt != nil:
		at = o.ShippedAt.Add(w.Pickup)
	case sh.Status == ShipmentDelivered && sh.DeliveredAt != nil:
		at = sh.DeliveredAt.Add(w.Complete)
	default:
		return nil
	}
	return &at
}

// NewShipmentDraft checks what the seller picked for a shipment: a carrier
// a seller can link, GHTK or GHN, the service, the weight and the pickup.
func NewShipmentDraft(carrier, service string, weightGrams int, pickup string) (ShipmentDraft, error) {
	e := ErrInvalidShipment
	c, s, p := Carrier(carrier), LinkedService(service), Pickup(pickup)
	if c != CarrierGHTK && c != CarrierGHN {
		e = e.WithField("carrier", apperr.NotAllowed, "ghtk or ghn")
	}
	if s != ServiceStandard && s != ServiceExpress {
		e = e.WithField("service", apperr.NotAllowed, "standard or express")
	}
	if weightGrams < 1 || weightGrams > MaxWeight {
		e = e.WithField("weight_grams", apperr.OutOfRange, "1 to 20,000 grams")
	}
	if p != PickupHome && p != PickupPostOffice {
		e = e.WithField("pickup", apperr.NotAllowed, "home or post_office")
	}
	if len(e.Fields) > 0 {
		return ShipmentDraft{}, e
	}
	return ShipmentDraft{Carrier: c, Service: s, WeightGrams: weightGrams, Pickup: p}, nil
}

// ParseShipmentStatus reads a status module shipping reports, lost
// included.
func ParseShipmentStatus(s string) (ShipmentStatus, error) {
	st := ShipmentStatus(s)
	switch st {
	case ShipmentPendingPickup, ShipmentInTransit, ShipmentDelivered, ShipmentFailed, ShipmentLost:
		return st, nil
	}
	return "", ErrInvalidStatus
}

// OpenStatuses are those of an order still under way: it counts against
// MaxOpenOrders and keeps its parties from deleting their accounts.
var OpenStatuses = []OrderStatus{OrderPending, OrderShipped}

// Open reports whether the order is in an OpenStatuses status.
func (o Order) Open() bool { return slices.Contains(OpenStatuses, o.Status) }

// BuyerOrders counts a buyer's orders: Placed and Failed over BuyerPeriod,
// Open now, against MaxOpenOrders.
type BuyerOrders struct {
	Placed, Failed, Open int
}

// RecipientFor is the recipient as viewer may read it: the seller of a
// cancelled order keeps only the name and the commune.
func (o Order) RecipientFor(viewer Party) Recipient {
	r := o.Recipient
	if viewer == Seller && o.Status == OrderCancelled {
		r.Phone, r.Street = "", ""
	}
	return r
}
