package domain

import (
	"embed"
	"strings"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/mailview"
)

//go:embed mail/*.json
var mailFiles embed.FS

// words is the mail's text in every language, mail/<tag>.json.
var words = locale.MustLoad(mailFiles, "mail")

// Mail is the mail of a notice of type t in language tag, its links on
// site: the facts and the one action the recipient needs. Only the types
// Mailed reports are written here; any other is an empty letter.
func Mail(t Type, p Params, site string, tag locale.Tag) mailview.Letter {
	say := func(key string, args ...any) string { return words.Say(tag, key, args...) }
	name := func(s string) string {
		if s == "" {
			return say("unnamed")
		}
		return s
	}
	actor := name(p.Actor)
	l := mailview.Letter{Locale: tag, Site: site, Art: mailview.ArtVerified, Group: say("group.account"), Why: say("why.account")}
	order := "/orders"
	if p.OrderID != (uuid.UUID{}) {
		order += "/" + p.OrderID.String()
	}
	if types[t].mail == mailOrders {
		side := say("side.seller", actor)
		if p.ToSeller {
			side = say("side.buyer", actor)
		}
		l.Item = &mailview.Item{Photo: p.Photo, Title: p.Title, Side: side}
		if p.Price > 0 {
			l.Item.Price = amount(tag, p.Price)
		}
		l.Group, l.Why, l.Settings = say("group.orders"), say("why.orders"), true
	}
	view := &mailview.Action{Label: say("action.view"), Path: order}
	switch t {
	case TypeCodRequested:
		l.Art, l.Subject, l.Title = mailview.ArtOrders, say("cod.subject", actor, p.Title), say("cod.title", actor)
		l.Lead, l.Note = say("cod.lead"), say("money.seller")
		l.Steps, l.Action = &mailview.Steps{Done: 1, Current: 1}, &mailview.Action{Label: say("cod.action"), Path: order}
	case TypeShipped:
		l.Art, l.Subject, l.Title = mailview.ArtShipped, say("shipped.subject", actor, p.Title), say("shipped.title", actor)
		l.Lead, l.Steps, l.Action = say("shipped.lead"), &mailview.Steps{Done: 2, Current: 2}, view
		l.Rows = tracking(p, say)
		if p.Price > 0 && p.Fee > 0 {
			l.Rows = append(l.Rows, mailview.Row{Label: say("row.pay"), Value: amount(tag, p.Price+p.Fee), Strong: true})
		}
	case TypeCompleted:
		l.Steps = &mailview.Steps{Done: 3, Current: 3}
		if p.ToSeller {
			l.Art, l.Subject, l.Title = mailview.ArtDeal, say("received.subject", actor, p.Title), say("received.title", actor)
			l.Lead, l.Note = say("received.lead"), say("money.seller")
			l.Action = &mailview.Action{Label: say("received.action"), Path: order}
			break
		}
		seller := name(p.Seller)
		l.Art, l.Subject, l.Title = mailview.ArtReceived, say("delivered.subject", p.Title, seller), say("delivered.title")
		l.Lead, l.Stars = say("delivered.lead", seller), order
	case TypeFailed:
		l.Art, l.Subject, l.Title = mailview.ArtNotFound, say("failed.subject", p.Title), say("failed.title")
		l.Lead, l.Action = say("failed.lead"), view
		if p.ToSeller {
			l.Lead, l.Rows, l.Note = say("failed.lead.seller"), tracking(p, say), say("failed.note.seller")
		}
	case TypeOrderCancelled:
		key := "cancelled." + p.Reason
		if p.ToSeller && p.Reason == "seller_timeout" {
			key += ".seller"
		}
		lead := say(key, p.Title)
		if lead == key {
			lead = say("cancelled.by", actor, p.Title)
		}
		l.Art, l.Subject, l.Title, l.Lead, l.Action = mailview.ArtSearch, lead, say("cancelled.title"), lead, view
		if !p.ToSeller {
			l.Note = say("cancelled.note")
			l.Action = &mailview.Action{Label: say("cancelled.action"), Path: mailview.SearchPath(p.Title)}
		}
	case TypeSuspended:
		l.Title = say("suspended.title")
		if until, err := time.Parse(time.RFC3339, p.Until); err == nil {
			l.Title = say("suspended.until", until.In(vietnam).Format(say("date")))
		}
		l.Art, l.Subject, l.Lead, l.Note = mailview.ArtOffline, l.Title, say("suspended.lead"), say("suspended.note")
		l.Action = &mailview.Action{Label: say("suspended.action"), Path: "/support"}
	case TypeUnsuspended:
		l.Art, l.Title, l.Lead = mailview.ArtPosted, say("unsuspended.title"), say("unsuspended.lead")
		l.Subject, l.Action = l.Title, &mailview.Action{Label: say("unsuspended.action"), Path: "/"}
	case TypeOffer, TypeCounter, TypeAgreed, TypeDeclined, TypeAgreementCancelled, TypeOfferVoided, TypeListingLive,
		TypeListingRejected, TypeFollowedListing, TypeReview, TypeReviewReply, TypeWatchMatched, TypePriceDropped:
		// Never mailed.
	default:
	}
	l.Preheader = l.Lead
	return l
}

// tracking is an order's carrier and tracking code as rows; none before it
// ships.
func tracking(p Params, say func(string, ...any) string) []mailview.Row {
	if p.Code == "" {
		return nil
	}
	carrier, ok := carrierLabels[p.Carrier]
	if !ok {
		carrier = p.Carrier
	}
	return []mailview.Row{{Label: say("row.carrier"), Value: carrier}, {Label: say("row.code"), Value: p.Code, Mono: true}}
}

// amount writes an amount in full with tag's digit grouping: "3.800.000 ₫".
func amount(tag locale.Tag, n int64) string {
	return strings.ReplaceAll(vnd(n), ".", words.Say(tag, "money.group"))
}
