package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

const (
	// MaxOpenOrders is how many orders a buyer may have requested or
	// shipped at once.
	MaxOpenOrders   = 3
	MaxFee          = 5_000_000
	maxCarrierName  = 50
	maxTrackingCode = 50
	// SourceSelf is a waybill the seller made on the carrier's own app.
	SourceSelf = "self"
)

type OrderStatus string

const (
	OrderRequested OrderStatus = "requested"
	OrderShipped   OrderStatus = "shipped"
	OrderDelivered OrderStatus = "delivered"
	OrderReturned  OrderStatus = "returned"
	OrderCancelled OrderStatus = "cancelled"
)

// CancelReason is why an order was cancelled.
type CancelReason string

const (
	CancelByBuyer  CancelReason = "buyer"
	CancelBySeller CancelReason = "seller"
	// CancelSentToAnother is a request whose listing the seller shipped to
	// another buyer.
	CancelSentToAnother CancelReason = "sent_to_another"
	// CancelListingClosed is a request whose listing was hidden, sold off
	// the app or deleted.
	CancelListingClosed CancelReason = "listing_closed"
)

type Carrier string

const (
	CarrierGHTK        Carrier = "ghtk"
	CarrierGHN         Carrier = "ghn"
	CarrierSPX         Carrier = "spx"
	CarrierJT          Carrier = "jt"
	CarrierViettelPost Carrier = "viettel_post"
	CarrierOther       Carrier = "other"
)

// Recipient is where the parcel goes. Phone is in the +84 form.
type Recipient struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Street      string `json:"street"`
	CommuneCode string `json:"commune_code"`
}

// Waybill is the parcel's waybill as the seller recorded it.
type Waybill struct {
	Source       string  `json:"source"`
	Carrier      Carrier `json:"carrier"`
	CarrierName  string  `json:"carrier_name,omitempty"`
	TrackingCode string  `json:"tracking_code"`
	Fee          int64   `json:"fee"`
}

// Order is a buyer's request for cash on delivery at the agreed price, and
// what became of it. Code is set by the store.
type Order struct {
	ID           uuid.UUID    `json:"id"`
	Code         string       `json:"code"`
	Status       OrderStatus  `json:"status"`
	Price        int64        `json:"price"`
	Recipient    Recipient    `json:"recipient"`
	Waybill      *Waybill     `json:"waybill,omitempty"`
	CancelledBy  Party        `json:"cancelled_by,omitempty"`
	CancelReason CancelReason `json:"cancel_reason,omitempty"`
	RequestedAt  time.Time    `json:"requested_at"`
	ShippedAt    *time.Time   `json:"shipped_at,omitempty"`
	ClosedAt     *time.Time   `json:"closed_at,omitempty"`
}

// NewWaybill checks a waybill as the seller typed it. carrierName names a
// carrier of kind other, and only then.
func NewWaybill(carrier Carrier, carrierName, trackingCode string, fee int64) (Waybill, error) {
	e := ErrInvalidWaybill
	carrierName, trackingCode = strings.TrimSpace(carrierName), strings.TrimSpace(trackingCode)
	switch carrier {
	case CarrierGHTK, CarrierGHN, CarrierSPX, CarrierJT, CarrierViettelPost:
		if carrierName != "" {
			e = e.WithField("carrier_name", "only with carrier other")
		}
	case CarrierOther:
		if n := utf8.RuneCountInString(carrierName); n < 1 || n > maxCarrierName {
			e = e.WithField("carrier_name", "1 to 50 characters")
		}
	default:
		e = e.WithField("carrier", "ghtk, ghn, spx, jt, viettel_post or other")
	}
	if n := utf8.RuneCountInString(trackingCode); n < 1 || n > maxTrackingCode {
		e = e.WithField("tracking_code", "1 to 50 characters")
	}
	if fee < 0 || fee > MaxFee {
		e = e.WithField("fee", "0 to 5,000,000 đồng")
	}
	if len(e.Fields) > 0 {
		return Waybill{}, e
	}
	return Waybill{Source: SourceSelf, Carrier: carrier, CarrierName: carrierName, TrackingCode: trackingCode, Fee: fee}, nil
}

// Open reports whether the order is requested or shipped.
func (o Order) Open() bool { return o.Status == OrderRequested || o.Status == OrderShipped }

// RecipientFor is the recipient as viewer may read it: the seller of an
// order cancelled before shipping keeps only the name and the commune.
func (o Order) RecipientFor(viewer Party) Recipient {
	r := o.Recipient
	if viewer == Seller && o.Status == OrderCancelled {
		r.Phone, r.Street = "", ""
	}
	return r
}
