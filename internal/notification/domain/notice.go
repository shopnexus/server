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
	TypeCompleted          Type = "completed"
	TypeFailed             Type = "failed"
	TypeOrderCancelled     Type = "order_cancelled"
	TypeListingLive        Type = "listing_live"
	TypeListingRejected    Type = "listing_rejected"
	TypeFollowedListing    Type = "followed_listing"
	TypeReview             Type = "review"
	TypeSuspended          Type = "suspended"
	TypeUnsuspended        Type = "unsuspended"
	TypeWatchMatched       Type = "watch_matched"
	TypePriceDropped       Type = "price_dropped"
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
	// Carrier is a carrier code (a notice from before 2026-11-12 may hold the
	// name a seller typed for another carrier, which reads as itself); Code is
	// the tracking code.
	Carrier string `json:"carrier,omitempty"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// ToSeller marks the seller's copy of a notice both sides get with
	// their own words.
	ToSeller bool `json:"to_seller,omitempty"`
	Stars    int  `json:"stars,omitempty"`
	// Until is the end of a suspension in RFC 3339, empty for none.
	Until string `json:"until,omitempty"`
	// WatchID, Query and Count are a search watch's notice: the watch, its
	// words and its new listings.
	WatchID uuid.UUID `json:"watch_id,omitzero"`
	Query   string    `json:"query,omitempty"`
	Count   int       `json:"count,omitempty"`
	// SavedPrice is the listing's price when the recipient saved it.
	SavedPrice int64 `json:"saved_price,omitempty"`
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
	// Carrier and TrackingCode are the latest order's shipment.
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
		case "completed":
			// The buyer's confirmation tells the seller; the system's
			// completion asks the buyer for a review.
			t = TypeCompleted
			if m.By == "system" {
				p.Seller = f.SellerName
				// The seller hears that the buyer has it, as from a confirmation.
				sp := p
				sp.Actor, sp.Seller = f.BuyerName, ""
				out = append(out, Notice{Recipient: f.SellerID, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: sp, At: at})
			}
		case "failed":
			// A failed delivery tells the seller too: the parcel comes back.
			t = TypeFailed
			out = append(out, Notice{Recipient: f.SellerID, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: p, At: at})
		case "order_cancelled":
			t, p.Reason = TypeOrderCancelled, f.CancelReason
			// A timeout puts the seller's listing back on sale; the seller hears
			// of it too.
			if m.By == "system" && (p.Reason == "seller_timeout" || p.Reason == "pickup_timeout") {
				sp := p
				sp.ToSeller = true
				out = append(out, Notice{Recipient: f.SellerID, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: sp, At: at})
			}
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

// FollowedListing tells a follower that a seller they follow put a new
// listing on sale. It collapses per seller, so a run of listings leaves one
// unread notice, of the newest.
func FollowedListing(follower, seller uuid.UUID, sellerName string, listing uuid.UUID, title string, at time.Time) Notice {
	return Notice{
		Recipient: follower, Type: TypeFollowedListing, CollapseKey: "seller:" + seller.String(), At: at,
		Params: Params{ListingID: listing, SellerID: seller, Seller: sellerName, Title: title},
	}
}

// WatchMatched tells a watcher how many new listings their watch has; a
// newer count replaces the unread notice of the watch.
func WatchMatched(recipient, watch uuid.UUID, query string, count int, at time.Time) Notice {
	return Notice{
		Recipient: recipient, Type: TypeWatchMatched, CollapseKey: "watch:" + watch.String(), At: at,
		Params: Params{WatchID: watch, Query: query, Count: count},
	}
}

// PriceDropped tells a saver of a new low of a saved listing.
func PriceDropped(recipient, listing uuid.UUID, title string, price, saved int64, at time.Time) Notice {
	return Notice{
		Recipient: recipient, Type: TypePriceDropped, CollapseKey: "saved:" + listing.String(), At: at,
		Params: Params{ListingID: listing, Title: title, Amount: price, SavedPrice: saved},
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
// open, which param names it, and the switch that stops their push.
type group struct {
	title, screen string
	id            func(Params) uuid.UUID
	push          Switch
}

var (
	haggling = group{"Trả giá", "deal", dealID, SwitchOffers}
	orders   = group{"Đơn hàng", "order", dealID, SwitchOrders}
	listings = group{"Tin đăng", "listing", listingID, SwitchNone}
	followed = group{"Tin đăng", "listing", listingID, SwitchFollowed}
	reviews  = group{"Đánh giá", "seller_reviews", func(p Params) uuid.UUID { return p.SellerID }, SwitchReviews}
	accounts = group{"Tài khoản", "account", func(Params) uuid.UUID { return uuid.UUID{} }, SwitchNone}
	searches = group{"Tìm kiếm", "search_watch", func(p Params) uuid.UUID { return p.WatchID }, SwitchNone}
	saved    = group{"Đã lưu", "listing", listingID, SwitchPriceCuts}
)

func dealID(p Params) uuid.UUID    { return p.DealID }
func listingID(p Params) uuid.UUID { return p.ListingID }

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
	TypeCompleted: {orders, func(p Params) string {
		if p.Seller != "" {
			return p.Title + " đã giao. Đánh giá " + p.Seller
		}
		return p.Actor + " đã nhận " + p.Title
	}},
	TypeFailed: {orders, func(p Params) string { return p.Title + " giao không thành" }},
	TypeOrderCancelled: {orders, func(p Params) string {
		switch p.Reason {
		case "shipped_to_another":
			return p.Title + " đã gửi cho người khác, đơn của bạn đã huỷ"
		case "listing_closed":
			return p.Title + " không còn bán, đơn của bạn đã huỷ"
		case "seller_timeout":
			if p.ToSeller {
				return "Bạn chưa tạo đơn " + p.Title + ", đơn đã huỷ"
			}
			return "Người bán chưa gửi " + p.Title + ", đơn của bạn đã huỷ"
		case "pickup_timeout":
			return "Hãng vận chuyển chưa lấy " + p.Title + ", đơn đã huỷ"
		default:
			return p.Actor + " huỷ đơn " + p.Title
		}
	}},
	TypeListingLive:     {listings, func(p Params) string { return p.Title + " đã lên" }},
	TypeListingRejected: {listings, func(p Params) string { return p.Title + " chưa lên được: một ảnh bị từ chối" }},
	TypeFollowedListing: {followed, func(p Params) string { return p.Seller + " vừa đăng tin mới: " + p.Title }},
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
	TypeWatchMatched: {searches, func(p Params) string {
		n := strconv.Itoa(p.Count)
		if p.Count >= 99 {
			n = "99+"
		}
		return "Có " + n + " tin mới cho “" + p.Query + "”"
	}},
	TypePriceDropped: {saved, func(p Params) string {
		return p.Title + " giảm còn " + money(p.Amount) + ", lúc lưu " + money(p.SavedPrice)
	}},
}

// carrierLabels name the carriers by their codes; an older notice may store
// a label, which reads as itself.
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

// SwitchOf is the switch that stops pushes of type t; SwitchNone for an
// unknown type.
func SwitchOf(t Type) Switch { return types[t].push }

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

// SupportBody is a support reply's push: the title names ShopNexus, so the
// body is the text alone, cut as ChatBody cuts it, or the photo count.
func SupportBody(text string, photos int) string {
	if text != "" {
		if r := []rune(text); len(r) > chatRunes {
			text = string(r[:chatRunes])
		}
		return text
	}
	return strconv.Itoa(photos) + " ảnh"
}

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
