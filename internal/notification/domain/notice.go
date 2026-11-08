// Package domain holds module notification's rules: which change tells whom
// what, and the Vietnamese text of each notice.
package domain

import (
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// Retention is how long a notice is kept.
const Retention = 90 * 24 * time.Hour

// MaxToken caps a push token's length.
const MaxToken = 4096

var (
	ErrNotFound     = apperr.New(apperr.KindNotFound, "notification_not_found", "no such notification")
	ErrInvalidToken = apperr.New(apperr.KindInvalid, "invalid_push_token", "a push token is 1 to 4096 characters")
)

// Type is what a notice says.
type Type string

const (
	TypeOffer              Type = "offer"
	TypeCounter            Type = "counter"
	TypeAgreed             Type = "agreed"
	TypeDeclined           Type = "declined"
	TypeAgreementCancelled Type = "agreement_cancelled"
	TypeOfferVoided        Type = "offer_voided"
	TypeCodRequested       Type = "cod_requested"
	TypeShipped            Type = "shipped"
	TypeDelivered          Type = "delivered"
	TypeReturned           Type = "returned"
	TypeOrderCancelled     Type = "order_cancelled"
	TypeListingLive        Type = "listing_live"
	TypeListingRejected    Type = "listing_rejected"
	TypeReview             Type = "review"
	TypeSuspended          Type = "suspended"
	TypeUnsuspended        Type = "unsuspended"
)

// Notice is one thing to tell one person. Params hold what its text is
// written from; CollapseKey, when set, makes it replace the person's unread
// notice with the same key.
type Notice struct {
	Recipient   uuid.UUID
	Type        Type
	CollapseKey string
	Params      Params
	At          time.Time
}

// Params are what a notice's text and target are written from, stored as
// JSON. A notice sets only the ones its type reads.
type Params struct {
	DealID    uuid.UUID `json:"deal_id,omitzero"`
	ListingID uuid.UUID `json:"listing_id,omitzero"`
	SellerID  uuid.UUID `json:"seller_id,omitzero"`
	Title     string    `json:"title,omitempty"`
	// Actor is the person who made the change.
	Actor  string `json:"actor,omitempty"`
	Seller string `json:"seller,omitempty"`
	Amount int64  `json:"amount,omitempty"`
	// Move is the deal move that voided an offer.
	Move string `json:"move,omitempty"`
	// Carrier is a carrier code, or the name the seller typed for another
	// carrier; Code is the tracking code.
	Carrier string `json:"carrier,omitempty"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Stars   int    `json:"stars,omitempty"`
	// Until is the end of a suspension in RFC 3339, empty for none.
	Until string `json:"until,omitempty"`
}

// Move is a deal move as notification reads it.
type Move struct {
	Kind   string
	By     string // buyer, seller or system
	Amount int64
}

// DealFacts is what a deal's new moves tell.
type DealFacts struct {
	DealID, BuyerID, SellerID    uuid.UUID
	Title, BuyerName, SellerName string
	Moves                        []Move
	// Carrier and TrackingCode are the latest order's waybill; Carrier is
	// a carrier code, or the name the seller typed for another carrier.
	Carrier, TrackingCode, CancelReason string
}

// FromMoves decides the notices of a deal's new moves. A move the other
// side should hear of tells them; a withdrawal tells no one.
func FromMoves(f DealFacts, at time.Time) []Notice {
	out := make([]Notice, 0, len(f.Moves))
	for _, m := range f.Moves {
		to, actor := f.SellerID, f.BuyerName
		if m.By != "buyer" {
			to, actor = f.BuyerID, f.SellerName
		}
		p := Params{DealID: f.DealID, Title: f.Title, Actor: actor, Amount: m.Amount}
		var t Type
		switch m.Kind {
		case "offer":
			t = TypeOffer
		case "counter":
			t = TypeCounter
		case "accept":
			t = TypeAgreed
		case "decline":
			t = TypeDeclined
		case "cancel":
			t = TypeAgreementCancelled
		case "price_changed", "offers_closed", "listing_closed":
			t, to, p.Move = TypeOfferVoided, f.BuyerID, m.Kind
		case "cod_requested":
			t = TypeCodRequested
		case "shipped":
			t, p.Carrier, p.Code = TypeShipped, f.Carrier, f.TrackingCode
		case "delivered":
			t, p.Seller = TypeDelivered, f.SellerName
		case "returned":
			t, p.Seller = TypeReturned, f.SellerName
		case "order_cancelled":
			t, p.Reason = TypeOrderCancelled, f.CancelReason
		default:
			continue
		}
		out = append(out, Notice{Recipient: to, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: p, At: at})
	}
	return out
}

// ListingNotice tells a seller their listing went on sale or was refused.
func ListingNotice(seller, listing uuid.UUID, title string, live bool, at time.Time) Notice {
	t := TypeListingRejected
	if live {
		t = TypeListingLive
	}
	return Notice{
		Recipient: seller, Type: t, CollapseKey: "listing:" + listing.String(), At: at,
		Params: Params{ListingID: listing, Title: title},
	}
}

// Reviewed tells a seller a buyer reviewed them.
func Reviewed(seller uuid.UUID, buyerName, title string, stars int, at time.Time) Notice {
	return Notice{
		Recipient: seller, Type: TypeReview, At: at,
		Params: Params{SellerID: seller, Actor: buyerName, Title: title, Stars: stars},
	}
}

// Suspended tells an account it is suspended, until a time in RFC 3339
// when until is not empty.
func Suspended(account uuid.UUID, until string, at time.Time) Notice {
	return Notice{Recipient: account, Type: TypeSuspended, At: at, Params: Params{Until: until}}
}

// Unsuspended tells an account it may write again.
func Unsuspended(account uuid.UUID, at time.Time) Notice {
	return Notice{Recipient: account, Type: TypeUnsuspended, At: at}
}

// Target is the screen a notice opens.
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// group is what notices of a kind share: the push's title, the screen they
// open and which param names it.
type group struct {
	title, screen string
	id            func(Params) uuid.UUID
}

var (
	haggling = group{"Trả giá", "deal", func(p Params) uuid.UUID { return p.DealID }}
	orders   = group{"Đơn hàng", "order", func(p Params) uuid.UUID { return p.DealID }}
	listings = group{"Tin đăng", "listing", func(p Params) uuid.UUID { return p.ListingID }}
	reviews  = group{"Đánh giá", "seller_reviews", func(p Params) uuid.UUID { return p.SellerID }}
	accounts = group{"Tài khoản", "account", func(Params) uuid.UUID { return uuid.UUID{} }}
)

// types describes each notice type: its group and its Vietnamese text.
var types = map[Type]struct {
	group

	text func(Params) string
}{
	TypeOffer: {haggling, func(p Params) string { return p.Actor + " trả " + money(p.Amount) + " cho " + p.Title }},
	TypeCounter: {haggling, func(p Params) string {
		return p.Actor + " trả lại " + money(p.Amount) + " cho " + p.Title
	}},
	TypeAgreed: {haggling, func(p Params) string { return "Chốt " + money(p.Amount) + " " + p.Title + " với " + p.Actor }},
	TypeDeclined: {haggling, func(p Params) string {
		return p.Actor + " không đồng ý giá bạn đề nghị cho " + p.Title
	}},
	TypeAgreementCancelled: {haggling, func(p Params) string { return p.Actor + " huỷ giá đã chốt " + p.Title }},
	TypeOfferVoided: {haggling, func(p Params) string {
		if p.Move == "listing_closed" {
			return p.Title + " không còn bán, giá bạn đề nghị hết hiệu lực"
		}
		return p.Title + " đã đổi, giá bạn đề nghị hết hiệu lực"
	}},
	TypeCodRequested: {orders, func(p Params) string { return p.Actor + " yêu cầu COD " + p.Title }},
	TypeShipped: {orders, func(p Params) string {
		carrier, ok := carrierLabels[p.Carrier]
		if !ok {
			carrier = p.Carrier
		}
		return p.Actor + " đã gửi " + p.Title + " · " + carrier + " " + p.Code
	}},
	TypeDelivered: {orders, func(p Params) string { return p.Title + " đã giao. Đánh giá " + p.Seller }},
	TypeReturned:  {orders, func(p Params) string { return p.Title + " bị hoàn về " + p.Seller }},
	TypeOrderCancelled: {orders, func(p Params) string {
		switch p.Reason {
		case "sent_to_another":
			return p.Title + " đã gửi cho người khác, đơn của bạn đã huỷ"
		case "listing_closed":
			return p.Title + " không còn bán, đơn của bạn đã huỷ"
		default:
			return p.Actor + " huỷ đơn " + p.Title
		}
	}},
	TypeListingLive:     {listings, func(p Params) string { return p.Title + " đã lên" }},
	TypeListingRejected: {listings, func(p Params) string { return p.Title + " chưa lên được: một ảnh bị từ chối" }},
	TypeReview: {reviews, func(p Params) string {
		return p.Actor + " chấm " + strconv.Itoa(p.Stars) + " sao cho " + p.Title
	}},
	TypeSuspended: {accounts, func(p Params) string {
		until, err := time.Parse(time.RFC3339, p.Until)
		if err != nil {
			return "Tài khoản của bạn bị khoá"
		}
		return "Tài khoản của bạn bị khoá đến " + until.In(vietnam).Format("2/1")
	}},
	TypeUnsuspended: {accounts, func(Params) string { return "Tài khoản của bạn đã được mở khoá" }},
}

// carrierLabels name the carriers by their codes; a notice written before
// the label moved here stores the label, which reads as itself.
var carrierLabels = map[string]string{
	"ghtk": "GHTK", "ghn": "GHN", "spx": "SPX", "jt": "J&T", "viettel_post": "Viettel Post",
}

// vietnam is the time zone dates are written in.
var vietnam = time.FixedZone("ICT", 7*60*60)

// Text writes a notice in Vietnamese; an unknown type has no text.
func Text(t Type, p Params) string {
	if d, ok := types[t]; ok {
		return d.text(p)
	}
	return ""
}

// Title is a push's title, by the notice's group.
func Title(t Type) string {
	if d, ok := types[t]; ok {
		return d.title
	}
	return "ShopNexus"
}

// TargetOf is the screen a notice of type t opens.
func TargetOf(t Type, p Params) Target {
	d, ok := types[t]
	if !ok {
		return Target{}
	}
	id := ""
	if v := d.id(p); v != (uuid.UUID{}) {
		id = v.String()
	}
	return Target{Kind: d.screen, ID: id}
}

// money writes an amount the way the app does: "1,2 tr" from a million,
// "350.000 ₫" below it.
func money(n int64) string {
	if n >= 1_000_000 {
		return strings.Replace(strconv.FormatFloat(float64(n)/1_000_000, 'f', -1, 64), ".", ",", 1) + " tr"
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String() + " ₫"
}

// chatRunes is how much of a message a chat push shows.
const chatRunes = 100

// ChatBody is a chat push's body: the sender and the first 100 characters
// of the text, or the photos they sent.
func ChatBody(sender, text string, photos int) string {
	if text != "" {
		if r := []rune(text); len(r) > chatRunes {
			text = string(r[:chatRunes])
		}
		return sender + ": " + text
	}
	return sender + " gửi " + strconv.Itoa(photos) + " ảnh"
}
