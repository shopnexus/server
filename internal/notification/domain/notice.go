// Package domain holds module notification's rules: which change tells whom
// what, and the Vietnamese text of each notice.
package domain

import (
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
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
	TypeReviewReply        Type = "review_reply"
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
	DealID uuid.UUID `json:"deal_id,omitzero"`
	// OrderID is the order an order notice is about. A notice of a step
	// that names no order (see DealFacts) has none and opens its deal.
	OrderID   uuid.UUID `json:"order_id,omitzero"`
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

// Move is a deal move as notification reads it. A step of an order names
// the order, with its shipment's carrier and tracking code and the reason
// it was cancelled, as the order holds them now.
type Move struct {
	Kind    string
	By      string // buyer, seller or system
	Amount  int64
	OrderID uuid.UUID

	Carrier, TrackingCode, CancelReason string
}

// DealFacts is what a deal's new moves tell. Carrier, TrackingCode and
// CancelReason are the deal's latest order's, which a move that names no
// order tells of: a step the release before wrote or migration 0052 could
// not match, and every move of a journal entry from before 2026-11-17,
// whose facts hold only these.
type DealFacts struct {
	DealID, BuyerID, SellerID    uuid.UUID
	Title, BuyerName, SellerName string
	Moves                        []Move

	Carrier, TrackingCode, CancelReason string
}

// FromMoves decides the notices of a deal's new moves. A move the other
// side should hear of tells them; a withdrawal tells no one.
func FromMoves(f DealFacts, at time.Time) []Notice {
	out := make([]Notice, 0, len(f.Moves))
	for _, m := range f.Moves {
		carrier, code, reason := m.Carrier, m.TrackingCode, m.CancelReason
		if m.OrderID == (uuid.UUID{}) {
			carrier, code, reason = f.Carrier, f.TrackingCode, f.CancelReason
		}
		to, actor := f.SellerID, f.BuyerName
		if m.By != "buyer" {
			to, actor = f.BuyerID, f.SellerName
		}
		p := Params{DealID: f.DealID, OrderID: m.OrderID, Title: f.Title, Actor: actor, Amount: m.Amount}
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
			t, p.Carrier, p.Code = TypeShipped, carrier, code
		case "completed":
			// The buyer's confirmation tells the seller; the system's
			// completion asks the buyer for a review.
			t = TypeCompleted
			if m.By == "system" {
				p.Seller = named(f.SellerName)
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
			t, p.Reason = TypeOrderCancelled, reason
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
		Params: Params{ListingID: listing, SellerID: seller, Seller: named(sellerName), Title: title},
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

// Reviewed tells the subject of a review, once it shows, that the other
// side of an order reviewed them.
func Reviewed(subject uuid.UUID, reviewerName, title string, stars int, order uuid.UUID, at time.Time) Notice {
	return Notice{
		Recipient: subject, Type: TypeReview, At: at,
		Params: Params{OrderID: order, Actor: reviewerName, Title: title, Stars: stars},
	}
}

// ReviewReplied tells a reviewer that the subject replied to their review.
func ReviewReplied(reviewer uuid.UUID, subjectName, title string, order uuid.UUID, at time.Time) Notice {
	return Notice{
		Recipient: reviewer, Type: TypeReviewReply, At: at,
		Params: Params{OrderID: order, Actor: subjectName, Title: title},
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
// open, the switch that stops their push and whether they are mailed.
type group struct {
	title  string
	target func(Params) Target
	push   Switch
	mail   mailing
}

// mailing says which notices are mailed as well as pushed.
type mailing uint8

const (
	mailNever mailing = iota
	// mailOrders mails while the order_emails switch is on.
	mailOrders
	// mailAlways mails whatever the settings say.
	mailAlways
)

var (
	haggling = group{"Trả giá", opens("deal", dealID), SwitchOffers, mailNever}
	orders   = group{"Đơn hàng", orderTarget, SwitchOrders, mailOrders}
	listings = group{"Tin đăng", opens("listing", listingID), SwitchNone, mailNever}
	followed = group{"Tin đăng", opens("listing", listingID), SwitchFollowed, mailNever}
	reviews  = group{"Đánh giá", reviewTarget, SwitchReviews, mailNever}
	accounts = group{"Tài khoản", opens("account", func(Params) uuid.UUID { return uuid.UUID{} }), SwitchNone, mailAlways}
	searches = group{"Tìm kiếm", opens("search_watch", func(p Params) uuid.UUID { return p.WatchID }), SwitchNone, mailNever}
	saved    = group{"Đã lưu", opens("listing", listingID), SwitchPriceCuts, mailNever}
)

func dealID(p Params) uuid.UUID    { return p.DealID }
func listingID(p Params) uuid.UUID { return p.ListingID }

// opens is the target of screen, named by the param id reads; a zero ID is
// empty.
func opens(screen string, id func(Params) uuid.UUID) func(Params) Target {
	return func(p Params) Target {
		t := Target{Kind: screen}
		if v := id(p); v != (uuid.UUID{}) {
			t.ID = v.String()
		}
		return t
	}
}

// orderTarget opens the order a notice is about. An order notice without
// its order, from before order IDs were kept, opens its deal: the deal's
// latest order may be a newer one.
func orderTarget(p Params) Target {
	if p.OrderID == (uuid.UUID{}) {
		return haggling.target(p)
	}
	return Target{Kind: "order", ID: p.OrderID.String()}
}

// reviewTarget opens the order a review notice is about; one from before
// two-way reviews names only the seller and opens their reviews.
func reviewTarget(p Params) Target {
	if p.OrderID == (uuid.UUID{}) {
		return opens("seller_reviews", func(p Params) uuid.UUID { return p.SellerID })(p)
	}
	return Target{Kind: "order", ID: p.OrderID.String()}
}

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
		return p.Actor + " chấm bạn " + strconv.Itoa(p.Stars) + " sao · " + p.Title
	}},
	TypeReviewReply: {reviews, func(p Params) string { return p.Actor + " trả lời đánh giá của bạn · " + p.Title }},
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

// unnamed stands for a person who has not set a display name, as in the apps.
const unnamed = "Người dùng"

func named(name string) string {
	if name == "" {
		return unnamed
	}
	return name
}

// Text writes a notice in Vietnamese; an unknown type has no text.
func Text(t Type, p Params) string {
	if d, ok := types[t]; ok {
		p.Actor = named(p.Actor)
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

// Mail is the subject and text of a notice's mail: the notice's text and a
// line on where to see more.
func Mail(t Type, p Params) (subject, text string) {
	subject = Text(t, p)
	text = subject + "\n\nMở ứng dụng ShopNexus để xem chi tiết."
	if types[t].mail == mailOrders {
		text += "\nBạn có thể tắt email đơn hàng trong Cài đặt thông báo."
	}
	return subject, text
}

// Mailed reports whether notices of type t are ever mailed; Settings.Mails
// says whether one is mailed to its recipient.
func Mailed(t Type) bool { return types[t].mail != mailNever }

// SwitchOf is the switch that stops pushes of type t; SwitchNone for an
// unknown type.
func SwitchOf(t Type) Switch { return types[t].push }

// TargetOf is the screen a notice of type t opens.
func TargetOf(t Type, p Params) Target {
	d, ok := types[t]
	if !ok {
		return Target{}
	}
	return d.target(p)
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
	sender = named(sender)
	if text != "" {
		if r := []rune(text); len(r) > chatRunes {
			text = string(r[:chatRunes])
		}
		return sender + ": " + text
	}
	return sender + " gửi " + strconv.Itoa(photos) + " ảnh"
}
