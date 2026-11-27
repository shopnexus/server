// Package domain holds module notification's rules: which change tells whom
// what, and the Vietnamese text of each notice.
package domain

import (
	"strconv"
	"time"
	"uuid"

	chototdomain "github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/locale"
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
	// TypeFeedbackMoved tells of a feedback moved to in_progress or
	// shipped, TypeFeedbackReplied of ShopNexus's first reply to one.
	TypeFeedbackMoved   Type = "feedback_moved"
	TypeFeedbackReplied Type = "feedback_replied"
	// TypeChototImported tells of listings a Chợ Tốt link brought over on
	// its own, TypeChototSoldHere of a copy sold here whose listing is
	// still on Chợ Tốt.
	TypeChototImported Type = "chotot_imported"
	TypeChototSoldHere Type = "chotot_sold_here"
	// TypeParcelLost tells both sides of a failed order that the carrier
	// lost the parcel, which counts against neither.
	TypeParcelLost Type = "parcel_lost"
	// TypeBoostStarted and the other boost types tell a seller what became
	// of a package bought for a listing (UC-LST-03).
	TypeBoostStarted  Type = "boost_started"
	TypeBoostFailed   Type = "boost_failed"
	TypeBoostExpired  Type = "boost_expired"
	TypeBoostRefunded Type = "boost_refunded"
	TypeBoostEnded    Type = "boost_ended"
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
	// Order is set on an order notice, for its mail.
	Order *OrderFacts
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
	// ToSeller marks an order notice to the seller.
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
	// FeedbackID, Status, Version and Excerpt are a feedback notice's: the
	// feedback, the status it moved to, the version that has it and the
	// start of ShopNexus's reply.
	FeedbackID uuid.UUID `json:"feedback_id,omitzero"`
	Status     string    `json:"status,omitempty"`
	Version    string    `json:"version,omitempty"`
	Excerpt    string    `json:"excerpt,omitempty"`
	// ListID is a listing on Chợ Tốt.
	ListID int64 `json:"list_id,omitempty"`
}

// OrderFacts are what an order notice's mail shows beyond its params: the
// listing's cover, the order's price and the carrier's fee, 0 when not
// known yet. They are not stored.
type OrderFacts struct {
	Photo      string
	Price, Fee int64
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
	// Price and Fee are the order's price and the carrier's fee, 0 when
	// the fee is not known yet.
	Price, Fee int64
	// Lost is a failed order whose parcel the carrier lost.
	Lost bool
}

// DealFacts is what a deal's new moves tell. Carrier, TrackingCode and
// CancelReason are the deal's latest order's, which a move that names no
// order tells of: a step the release before wrote or migration 0052 could
// not match, and every move of a journal entry from before 2026-11-17,
// whose facts hold only these.
type DealFacts struct {
	DealID, BuyerID, SellerID    uuid.UUID
	Title, BuyerName, SellerName string
	// Photo is the listing's cover thumbnail.
	Photo string
	Moves []Move

	Carrier, TrackingCode, CancelReason string
	// SellerIgnores and BuyerIgnores are whether that side ignores the
	// pair's conversation: an offer or a counter from the other side tells
	// them nothing. What moves the order still tells them.
	SellerIgnores, BuyerIgnores bool
}

// FromMoves decides the notices of a deal's new moves. A move the other
// side should hear of tells them; a withdrawal tells no one, nor does a
// bid to a side that ignores the bidder.
func FromMoves(f DealFacts, at time.Time) []Notice {
	out := make([]Notice, 0, len(f.Moves))
	for i := range f.Moves {
		m := &f.Moves[i]
		carrier, code, reason := m.Carrier, m.TrackingCode, m.CancelReason
		if m.OrderID == (uuid.UUID{}) {
			carrier, code, reason = f.Carrier, f.TrackingCode, f.CancelReason
		}
		to, actor := f.SellerID, f.BuyerName
		if m.By != "buyer" {
			to, actor = f.BuyerID, f.SellerName
		}
		p := Params{DealID: f.DealID, OrderID: m.OrderID, Title: f.Title, Actor: actor, Amount: m.Amount}
		var order *OrderFacts
		if m.OrderID != (uuid.UUID{}) {
			p.ToSeller, order = to == f.SellerID, &OrderFacts{Photo: f.Photo, Price: m.Price, Fee: m.Fee}
		}
		var t Type
		switch m.Kind {
		case "offer", "counter":
			if (to == f.SellerID && f.SellerIgnores) || (to == f.BuyerID && f.BuyerIgnores) {
				continue
			}
			t = TypeOffer
			if m.Kind == "counter" {
				t = TypeCounter
			}
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
				sp.Actor, sp.Seller, sp.ToSeller = f.BuyerName, "", true
				out = append(out, Notice{Recipient: f.SellerID, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: sp, At: at, Order: order})
			}
		case "failed":
			// A failed delivery tells the seller too: the parcel comes back,
			// or, lost by the carrier, is the seller's to claim there.
			t, p.Carrier, p.Code = TypeFailed, carrier, code
			if m.Lost {
				t = TypeParcelLost
			}
			sp := p
			sp.Actor, sp.ToSeller = f.BuyerName, true
			out = append(out, Notice{Recipient: f.SellerID, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: sp, At: at, Order: order})
		case "order_cancelled":
			t, p.Reason = TypeOrderCancelled, reason
			// A timeout puts the seller's listing back on sale; the seller hears
			// of it too.
			if m.By == "system" && (p.Reason == "seller_timeout" || p.Reason == "pickup_timeout") {
				sp := p
				sp.Actor, sp.ToSeller = f.BuyerName, true
				out = append(out, Notice{Recipient: f.SellerID, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: sp, At: at, Order: order})
			}
		default:
			continue
		}
		out = append(out, Notice{Recipient: to, Type: t, CollapseKey: "deal:" + f.DealID.String(), Params: p, At: at, Order: order})
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

// ChototImported tells a seller how many listings their Chợ Tốt link
// brought over; a newer one replaces the unread notice.
func ChototImported(recipient uuid.UUID, count int, at time.Time) Notice {
	return Notice{
		Recipient: recipient, Type: TypeChototImported, CollapseKey: "chotot_imported", At: at, Params: Params{Count: count},
	}
}

// ChototSoldHere reminds a seller to hide on Chợ Tốt the listing whose
// copy sold here.
func ChototSoldHere(recipient, listing uuid.UUID, title string, listID int64, at time.Time) Notice {
	return Notice{
		Recipient: recipient, Type: TypeChototSoldHere, At: at, Params: Params{ListingID: listing, Title: title, ListID: listID},
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

// FeedbackMoved tells a feedback's owner that ShopNexus works on it, or
// that version has it; version may be empty.
func FeedbackMoved(owner, feedback uuid.UUID, status, version string, at time.Time) Notice {
	return Notice{
		Recipient: owner, Type: TypeFeedbackMoved, CollapseKey: "feedback:" + feedback.String(), At: at,
		Params: Params{FeedbackID: feedback, Status: status, Version: version},
	}
}

// FeedbackReplied tells a feedback's owner of ShopNexus's reply, by its
// first 100 characters.
func FeedbackReplied(owner, feedback uuid.UUID, reply string, at time.Time) Notice {
	if r := []rune(reply); len(r) > chatRunes {
		reply = string(r[:chatRunes])
	}
	return Notice{
		Recipient: owner, Type: TypeFeedbackReplied, CollapseKey: "feedback:" + feedback.String(), At: at,
		Params: Params{FeedbackID: feedback, Excerpt: reply},
	}
}

// Boost tells a seller of typ on the package of days, bought at price for
// their listing; a later notice of the same boost replaces an unread one.
func Boost(recipient, boost, listing uuid.UUID, typ Type, title string, days int, price int64, at time.Time) Notice {
	return Notice{
		Recipient: recipient, Type: typ, CollapseKey: "boost:" + boost.String(), At: at,
		Params: Params{ListingID: listing, Title: title, Count: days, Amount: price},
	}
}

// Target is the screen a notice opens.
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// group is what notices of a kind share: the key of the push's title, the
// screen they open, the switch that stops their push and whether they are
// mailed.
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

// titleListings titles the pushes about the recipient's listings.
const titleListings = "title.listings"

var (
	haggling = group{"title.offers", opens("deal", dealID), SwitchOffers, mailNever}
	orders   = group{"title.orders", orderTarget, SwitchOrders, mailOrders}
	listings = group{titleListings, opens("listing", listingID), SwitchNone, mailNever}
	followed = group{titleListings, opens("listing", listingID), SwitchFollowed, mailNever}
	reviews  = group{"title.reviews", reviewTarget, SwitchReviews, mailNever}
	accounts = group{"title.account", opens("account", func(Params) uuid.UUID { return uuid.UUID{} }), SwitchNone, mailAlways}
	searches = group{"title.searches", opens("search_watch", func(p Params) uuid.UUID { return p.WatchID }), SwitchNone, mailNever}
	saved    = group{"title.saved", opens("listing", listingID), SwitchPriceCuts, mailNever}
	feedback = group{"title.feedback", opens("feedback", func(p Params) uuid.UUID { return p.FeedbackID }), SwitchNone, mailNever}
	// chototLink opens the Chợ Tốt link, chototAd the listing on Chợ Tốt.
	chototLink = group{titleListings, func(Params) Target { return Target{Kind: "chotot"} }, SwitchNone, mailNever}
	chototAd   = group{
		titleListings, func(p Params) Target { return Target{Kind: "chotot_ad", ID: chototdomain.AdURL(p.ListID)} },
		SwitchNone, mailNever,
	}
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

// types describes each notice type: its group and how its text is written.
var types = map[Type]struct {
	group

	text func(Params, writer) string
}{
	TypeOffer:    {haggling, func(p Params, w writer) string { return w.say("notice.offer", p.Actor, w.money(p.Amount), p.Title) }},
	TypeCounter:  {haggling, func(p Params, w writer) string { return w.say("notice.counter", p.Actor, w.money(p.Amount), p.Title) }},
	TypeAgreed:   {haggling, func(p Params, w writer) string { return w.say("notice.agreed", w.money(p.Amount), p.Title, p.Actor) }},
	TypeDeclined: {haggling, func(p Params, w writer) string { return w.say("notice.declined", p.Actor, p.Title) }},
	TypeAgreementCancelled: {haggling, func(p Params, w writer) string {
		return w.say("notice.agreement_cancelled", p.Actor, p.Title)
	}},
	TypeOfferVoided: {haggling, func(p Params, w writer) string {
		if p.Move == "listing_closed" {
			return w.say("notice.offer_voided.listing_closed", p.Title)
		}
		return w.say("notice.offer_voided", p.Title)
	}},
	TypeCodRequested: {orders, func(p Params, w writer) string { return w.say("notice.cod_requested", p.Actor, p.Title) }},
	TypeShipped: {orders, func(p Params, w writer) string {
		return w.say("notice.shipped", p.Actor, p.Title, carrierName(p.Carrier), p.Code)
	}},
	TypeCompleted: {orders, func(p Params, w writer) string {
		if p.Seller != "" {
			return w.say("notice.completed.review", p.Title, p.Seller)
		}
		return w.say("notice.completed", p.Actor, p.Title)
	}},
	TypeFailed: {orders, func(p Params, w writer) string { return w.say("notice.failed", p.Title) }},
	TypeParcelLost: {orders, func(p Params, w writer) string {
		if p.ToSeller {
			return w.say("notice.parcel_lost.seller", p.Title, carrierName(p.Carrier))
		}
		return w.say("notice.parcel_lost", p.Title, carrierName(p.Carrier))
	}},
	TypeOrderCancelled: {orders, func(p Params, w writer) string {
		switch p.Reason {
		case "shipped_to_another":
			return w.say("notice.cancelled.shipped_to_another", p.Title)
		case "listing_closed":
			return w.say("notice.cancelled.listing_closed", p.Title)
		case "pickup_timeout":
			return w.say("notice.cancelled.pickup_timeout", p.Title)
		case "seller_timeout":
			if p.ToSeller {
				return w.say("notice.cancelled.seller_timeout.seller", p.Title)
			}
			return w.say("notice.cancelled.seller_timeout", p.Title)
		default:
			return w.say("notice.cancelled.by", p.Actor, p.Title)
		}
	}},
	TypeListingLive:     {listings, func(p Params, w writer) string { return w.say("notice.listing_live", p.Title) }},
	TypeListingRejected: {listings, func(p Params, w writer) string { return w.say("notice.listing_rejected", p.Title) }},
	TypeFollowedListing: {followed, func(p Params, w writer) string { return w.say("notice.followed_listing", p.Seller, p.Title) }},
	TypeReview:          {reviews, func(p Params, w writer) string { return w.say("notice.review", p.Actor, p.Stars, p.Title) }},
	TypeReviewReply:     {reviews, func(p Params, w writer) string { return w.say("notice.review_reply", p.Actor, p.Title) }},
	TypeSuspended: {accounts, func(p Params, w writer) string {
		until, err := time.Parse(time.RFC3339, p.Until)
		if err != nil {
			return w.say("notice.suspended")
		}
		return w.say("notice.suspended.until", locale.Date(w.tag, until))
	}},
	TypeUnsuspended: {accounts, func(_ Params, w writer) string { return w.say("notice.unsuspended") }},
	TypeWatchMatched: {searches, func(p Params, w writer) string {
		n := strconv.Itoa(p.Count)
		if p.Count >= 99 {
			n = "99+"
		}
		return w.say("notice.watch_matched", n, p.Query)
	}},
	TypePriceDropped: {saved, func(p Params, w writer) string {
		return w.say("notice.price_dropped", p.Title, w.money(p.Amount), w.money(p.SavedPrice))
	}},
	TypeFeedbackMoved: {feedback, func(p Params, w writer) string {
		switch {
		case p.Status != "shipped":
			return w.say("notice.feedback_moved.in_progress")
		case p.Version != "":
			return w.say("notice.feedback_moved.shipped.version", p.Version)
		default:
			return w.say("notice.feedback_moved.shipped")
		}
	}},
	TypeFeedbackReplied: {feedback, func(p Params, w writer) string { return w.say("notice.feedback_replied", p.Excerpt) }},
	TypeChototImported:  {chototLink, func(p Params, w writer) string { return w.say("notice.chotot_imported", p.Count) }},
	TypeChototSoldHere:  {chototAd, func(p Params, w writer) string { return w.say("notice.chotot_sold_here", p.Title) }},
	TypeBoostStarted:    {listings, func(p Params, w writer) string { return w.say("notice.boost_started", p.Title, p.Count) }},
	TypeBoostFailed:     {listings, func(p Params, w writer) string { return w.say("notice.boost_failed", p.Title) }},
	TypeBoostExpired:    {listings, func(p Params, w writer) string { return w.say("notice.boost_expired", p.Title) }},
	TypeBoostRefunded:   {listings, func(p Params, w writer) string { return w.say("notice.boost_refunded", w.money(p.Amount), p.Title) }},
	TypeBoostEnded:      {listings, func(p Params, w writer) string { return w.say("notice.boost_ended", p.Title) }},
}

// carrierLabels name the carriers by their codes.
var carrierLabels = map[string]string{
	"ghtk": "GHTK", "ghn": "GHN", "spx": "SPX", "jt": "J&T", "viettel_post": "Viettel Post",
}

// carrierName names a carrier by its code; an older notice may store a
// label, which reads as itself.
func carrierName(code string) string {
	if name, ok := carrierLabels[code]; ok {
		return name
	}
	return code
}

// Text writes a notice in language tag; an unknown type has no text.
func Text(t Type, p Params, tag locale.Tag) string {
	if d, ok := types[t]; ok {
		w := writerFor(tag)
		p.Actor = w.name(p.Actor)
		return d.text(p, w)
	}
	return ""
}

// MessageTitle is a chat message push's title in language tag.
func MessageTitle(tag locale.Tag) string { return writerFor(tag).say("title.message") }

// Title is a push's title in language tag, by the notice's group.
func Title(t Type, tag locale.Tag) string {
	if d, ok := types[t]; ok {
		return writerFor(tag).say(d.title)
	}
	return "ShopNexus"
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

// chatRunes is how much of a message a chat push shows.
const chatRunes = 100

// SupportBody is a support reply's push: the title names ShopNexus, so the
// body is the text alone, cut as ChatBody cuts it, or the photo count.
func SupportBody(text string, photos int, tag locale.Tag) string {
	if text != "" {
		if r := []rune(text); len(r) > chatRunes {
			text = string(r[:chatRunes])
		}
		return text
	}
	return writerFor(tag).say("push.support_photos", photos)
}

// ChatBody is a chat push's body: the sender and the first 100 characters
// of the text, or the photos they sent.
func ChatBody(sender, text string, photos int, tag locale.Tag) string {
	w := writerFor(tag)
	sender = w.name(sender)
	if text != "" {
		if r := []rune(text); len(r) > chatRunes {
			text = string(r[:chatRunes])
		}
		return sender + ": " + text
	}
	return w.say("push.chat_photos", sender, photos)
}
