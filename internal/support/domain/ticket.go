// Package domain holds module support's rules: what a ticket of each kind
// needs, when an order may be reported, and how many tickets a person opens
// in a day.
package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	chatdomain "github.com/shopnexus/server2/internal/chat/domain"
)

// Kind is what a ticket is about.
type Kind string

const (
	// KindNotReceived is a buyer's "Báo chưa nhận hàng" on an order.
	KindNotReceived Kind = "not_received"
	// KindCarrierRequest is a seller asking for a carrier ShopNexus cannot
	// read yet.
	KindCarrierRequest Kind = "carrier_request"
	// KindOther is anything else, feedback included.
	KindOther Kind = "other"
)

// Status is whether a ticket waits for ShopNexus.
type Status string

const (
	StatusOpen     Status = "open"
	StatusResolved Status = "resolved"
)

// From is who wrote a ticket's last message: the person asks for an
// answer.
type From string

const (
	FromPerson    From = "person"
	FromShopNexus From = "shopnexus"
)

const (
	// DailyLimit is how many tickets an account opens in 24 hours.
	DailyLimit = 10
	// ReportWindow is how long after the system completed an order its
	// buyer may still report the parcel did not arrive.
	ReportWindow = 7 * 24 * time.Hour
	// SameCarrierWindow is how far back the console counts requests for
	// one carrier.
	SameCarrierWindow = 30 * 24 * time.Hour
	maxCarrierName    = 50
)

// Draft is a ticket as its person asked for it, checked.
type Draft struct {
	Kind        Kind
	OrderID     *uuid.UUID
	CarrierName string
	// CarrierKey is CarrierName folded, for counting one carrier's requests.
	CarrierKey string
	// Body is the first message's text, trimmed; it may be empty except for
	// KindOther.
	Body string
}

// NewTicket checks a request for a ticket: not_received needs its order,
// carrier_request a carrier name of 1 to 50 characters, other a body; a
// body is at most chat's 1,000 characters. Every broken field is named.
func NewTicket(kind, carrierName, body string, order *uuid.UUID) (Draft, error) {
	e := ErrInvalidTicket
	d := Draft{
		Kind: Kind(kind), OrderID: order, CarrierName: strings.TrimSpace(carrierName), Body: strings.TrimSpace(body),
	}
	switch d.Kind {
	case KindNotReceived:
		if order == nil {
			e = e.WithField("order_id", "the order whose parcel did not arrive")
		}
	case KindCarrierRequest:
		if n := utf8.RuneCountInString(d.CarrierName); n < 1 || n > maxCarrierName {
			e = e.WithField("carrier_name", "1 to 50 characters")
		}
		d.CarrierKey = CarrierKey(d.CarrierName)
	case KindOther:
		if d.Body == "" {
			e = e.WithField("body", "1 to 1,000 characters")
		}
	default:
		e = e.WithField("kind", "not_received, carrier_request or other")
	}
	if d.Kind != KindNotReceived && order != nil {
		e = e.WithField("order_id", "only for not_received")
	}
	if utf8.RuneCountInString(d.Body) > chatdomain.MaxText {
		e = e.WithField("body", "1 to 1,000 characters")
	}
	if len(e.Fields) > 0 {
		return Draft{}, e
	}
	if d.Kind != KindCarrierRequest {
		d.CarrierName = ""
	}
	return d, nil
}

// CarrierKey folds a carrier's name as discovery's HistoryKey folds a
// query: lowercase, spaces collapsed, Vietnamese marks dropped, đ as d, so
// "Giao Hàng  Nhanh" and "giao hang nhanh" count as one carrier.
func CarrierKey(name string) string {
	key := strings.ToLower(strings.Join(strings.Fields(name), " "))
	// A transformer keeps state, so each call builds its own.
	folded, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), key)
	if err != nil {
		folded = key
	}
	return strings.ReplaceAll(folded, "đ", "d")
}

// OrderCheck is what a report needs of an order, as module deal tells it.
type OrderCheck struct {
	BuyerID     uuid.UUID
	Status      string
	CompletedBy string
	ClosedAt    *time.Time
}

// MayReport says whether buyer may report that o's parcel did not arrive:
// while it is shipped, or within ReportWindow of the system completing it.
// Anyone but the buyer gets ErrOrderNotFound, so another's order does not
// show.
func MayReport(o OrderCheck, buyer uuid.UUID, now time.Time) error {
	switch {
	case o.BuyerID != buyer:
		return ErrOrderNotFound
	case o.Status == "shipped":
		return nil
	case o.Status == "completed" && o.CompletedBy == "system" && o.ClosedAt != nil && now.Sub(*o.ClosedAt) < ReportWindow:
		return nil
	default:
		return ErrNotAllowed
	}
}
