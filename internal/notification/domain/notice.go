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
	Params      map[string]any
	At          time.Time
}

// Move is a deal move as notification reads it.
type Move struct {
	Kind   string
	By     string // buyer, seller or system
	Amount int64
}

// DealFacts is what a deal's new moves tell.
type DealFacts struct {
	DealID, ListingID, BuyerID, SellerID uuid.UUID
	Title, BuyerName, SellerName         string
	Moves                                []Move
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
			t, to = TypeOfferVoided, f.BuyerID
		case "cod_requested":
			t = TypeCodRequested
		case "shipped":
			t = TypeShipped
		case "delivered":
			t = TypeDelivered
		case "returned":
			t = TypeReturned
		case "order_cancelled":
			t = TypeOrderCancelled
		default:
			continue
		}
		out = append(out, Notice{
			Recipient: to, Type: t, CollapseKey: "deal:" + f.DealID.String(), At: at,
			Params: map[string]any{
				"deal_id": f.DealID.String(), "title": f.Title, "actor": actor, "seller": f.SellerName,
				"amount": m.Amount, "move": m.Kind, "by": m.By, "carrier": carrierLabel(f.Carrier),
				"code": f.TrackingCode, "reason": f.CancelReason,
			},
		})
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
		Params: map[string]any{"listing_id": listing.String(), "title": title},
	}
}

// Reviewed tells a seller a buyer reviewed them.
func Reviewed(seller, review uuid.UUID, buyerName, title string, stars int, at time.Time) Notice {
	return Notice{
		Recipient: seller, Type: TypeReview, At: at,
		Params: map[string]any{
			"review_id": review.String(), "seller_id": seller.String(), "actor": buyerName, "title": title, "stars": stars,
		},
	}
}

// Suspended tells an account it is suspended, until a time in RFC 3339
// when until is not empty.
func Suspended(account uuid.UUID, until string, at time.Time) Notice {
	return Notice{Recipient: account, Type: TypeSuspended, At: at, Params: map[string]any{"until": until}}
}

// Unsuspended tells an account it may write again.
func Unsuspended(account uuid.UUID, at time.Time) Notice {
	return Notice{Recipient: account, Type: TypeUnsuspended, At: at, Params: map[string]any{}}
}

var carrierLabels = map[string]string{
	"ghtk": "GHTK", "ghn": "GHN", "spx": "SPX", "jt": "J&T", "viettel_post": "Viettel Post",
}

func carrierLabel(c string) string {
	if label, ok := carrierLabels[c]; ok {
		return label
	}
	return c
}

func str(p map[string]any, key string) string {
	if s, ok := p[key].(string); ok {
		return s
	}
	return ""
}

// number reads a JSON number back: an int64 before storing, a float64
// after.
func number(p map[string]any, key string) int64 {
	switch v := p[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
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

var texts = map[Type]func(p map[string]any) string{
	TypeOffer: func(p map[string]any) string {
		return str(p, "actor") + " trả " + money(number(p, "amount")) + " cho " + str(p, "title")
	},
	TypeCounter: func(p map[string]any) string {
		return str(p, "actor") + " trả lại " + money(number(p, "amount")) + " cho " + str(p, "title")
	},
	TypeAgreed: func(p map[string]any) string {
		return "Chốt " + money(number(p, "amount")) + " " + str(p, "title") + " với " + str(p, "actor")
	},
	TypeDeclined: func(p map[string]any) string {
		return str(p, "actor") + " không đồng ý giá bạn đề nghị cho " + str(p, "title")
	},
	TypeAgreementCancelled: func(p map[string]any) string { return str(p, "actor") + " huỷ giá đã chốt " + str(p, "title") },
	TypeOfferVoided: func(p map[string]any) string {
		if str(p, "move") == "listing_closed" {
			return str(p, "title") + " không còn bán, giá bạn đề nghị hết hiệu lực"
		}
		return str(p, "title") + " đã đổi, giá bạn đề nghị hết hiệu lực"
	},
	TypeCodRequested: func(p map[string]any) string { return str(p, "actor") + " yêu cầu COD " + str(p, "title") },
	TypeShipped: func(p map[string]any) string {
		return str(p, "actor") + " đã gửi " + str(p, "title") + " · " + str(p, "carrier") + " " + str(p, "code")
	},
	TypeDelivered: func(p map[string]any) string { return str(p, "title") + " đã giao. Đánh giá " + str(p, "seller") },
	TypeReturned:  func(p map[string]any) string { return str(p, "title") + " bị hoàn về " + str(p, "seller") },
	TypeOrderCancelled: func(p map[string]any) string {
		switch str(p, "reason") {
		case "sent_to_another":
			return str(p, "title") + " đã gửi cho người khác, đơn của bạn đã huỷ"
		case "listing_closed":
			return str(p, "title") + " không còn bán, đơn của bạn đã huỷ"
		default:
			return str(p, "actor") + " huỷ đơn " + str(p, "title")
		}
	},
	TypeListingLive: func(p map[string]any) string { return str(p, "title") + " đã lên" },
	TypeListingRejected: func(p map[string]any) string {
		return str(p, "title") + " chưa lên được: một ảnh bị từ chối"
	},
	TypeReview: func(p map[string]any) string {
		return str(p, "actor") + " chấm " + strconv.FormatInt(number(p, "stars"), 10) + " sao cho " + str(p, "title")
	},
	TypeSuspended: func(p map[string]any) string {
		until, err := time.Parse(time.RFC3339, str(p, "until"))
		if err != nil {
			return "Tài khoản của bạn bị khoá"
		}
		return "Tài khoản của bạn bị khoá đến " + until.In(vietnam).Format("2/1")
	},
	TypeUnsuspended: func(map[string]any) string { return "Tài khoản của bạn đã được mở khoá" },
}

// vietnam is the time zone dates are written in.
var vietnam = time.FixedZone("ICT", 7*60*60)

// Text writes a notice in Vietnamese.
func Text(t Type, p map[string]any) string {
	if write, ok := texts[t]; ok {
		return write(p)
	}
	return ""
}

// Title is a push's title, by the notice's group.
func Title(t Type) string {
	switch t {
	case TypeOffer, TypeCounter, TypeAgreed, TypeDeclined, TypeAgreementCancelled, TypeOfferVoided:
		return "Trả giá"
	case TypeCodRequested, TypeShipped, TypeDelivered, TypeReturned, TypeOrderCancelled:
		return "Đơn hàng"
	case TypeListingLive, TypeListingRejected:
		return "Tin đăng"
	case TypeReview:
		return "Đánh giá"
	case TypeSuspended, TypeUnsuspended:
		return "Tài khoản"
	default:
		return "ShopNexus"
	}
}

// Target is the screen a notice opens.
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// TargetOf is the screen a notice of type t opens.
func TargetOf(t Type, p map[string]any) Target {
	switch t {
	case TypeOffer, TypeCounter, TypeAgreed, TypeDeclined, TypeAgreementCancelled, TypeOfferVoided:
		return Target{Kind: "deal", ID: str(p, "deal_id")}
	case TypeCodRequested, TypeShipped, TypeDelivered, TypeReturned, TypeOrderCancelled:
		return Target{Kind: "order", ID: str(p, "deal_id")}
	case TypeListingLive, TypeListingRejected:
		return Target{Kind: "listing", ID: str(p, "listing_id")}
	case TypeReview:
		return Target{Kind: "seller_reviews", ID: str(p, "seller_id")}
	case TypeSuspended, TypeUnsuspended:
		return Target{Kind: "account"}
	default:
		return Target{}
	}
}

// ChatBody is a chat push's body: the sender and the start of the text, or
// the photos they sent.
func ChatBody(sender, text string, photos int) string {
	if text != "" {
		return sender + ": " + text
	}
	return sender + " gửi " + strconv.Itoa(photos) + " ảnh"
}
