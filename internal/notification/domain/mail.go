package domain

import (
	"net/url"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/mailview"
)

// Mail is the mail of notice n in language tag: the facts and the one action
// the recipient needs. Only the types Mailed reports are written here; any
// other is an empty letter.
func Mail(n Notice, tag locale.Tag) mailview.Letter {
	w := penFor(tag)
	tag, p, say, name := w.tag, n.Params, w.say, w.name
	actor := name(p.Actor)
	l := mailview.Letter{Locale: tag, Art: mailview.ArtVerified, Group: say("group.account"), Why: say("why.account")}
	order := "/orders"
	if p.OrderID != (uuid.UUID{}) {
		order += "/" + p.OrderID.String()
	}
	if types[n.Type].mail == mailOrders {
		side := say("side.seller", actor)
		if p.ToSeller {
			side = say("side.buyer", actor)
		}
		l.Item = &mailview.Item{Title: p.Title, Side: side}
		if o := n.Order; o != nil {
			l.Item.Photo = o.Photo
			if o.Price > 0 {
				l.Item.Price = locale.Money(tag, o.Price)
			}
		}
		l.Group, l.Why = say("group.orders"), say("why.orders")
		l.Footer = &mailview.Action{Label: say("footer.settings"), Path: "/notifications#settings"}
	}
	steps := func(at int) *mailview.Steps {
		return &mailview.Steps{Names: []string{say("steps.agreed"), say("steps.ordered"), say("steps.received"), say("steps.reviewed")}, At: at}
	}
	tracking := func() []mailview.Row {
		if p.Code == "" {
			return nil
		}
		return []mailview.Row{{Label: say("row.carrier"), Value: carrierName(p.Carrier)}, {Label: say("row.code"), Value: p.Code, Mono: true}}
	}
	view := &mailview.Action{Label: say("action.view"), Path: order}
	switch n.Type {
	case TypeCodRequested:
		l.Art, l.Subject, l.Title = mailview.ArtOrders, say("cod.subject", actor, p.Title), say("cod.title", actor)
		l.Lead, l.Note, l.Steps = say("cod.lead"), say("money.seller"), steps(1)
		l.Action = &mailview.Action{Label: say("cod.action"), Path: order}
	case TypeShipped:
		l.Art, l.Subject, l.Title = mailview.ArtShipped, say("shipped.subject", actor, p.Title), say("shipped.title", actor)
		l.Lead, l.Steps, l.Action, l.Rows = say("shipped.lead"), steps(2), view, tracking()
		if o := n.Order; o != nil && o.Price > 0 && o.Fee > 0 {
			l.Rows = append(l.Rows, mailview.Row{Label: say("row.pay"), Value: locale.Money(tag, o.Price+o.Fee), Strong: true})
		}
	case TypeCompleted:
		l.Steps = steps(3)
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
			l.Lead, l.Rows, l.Note = say("failed.lead.seller"), tracking(), say("failed.note.seller")
		}
	case TypeOrderCancelled:
		lead := Text(n.Type, p, tag) + "."
		l.Art, l.Subject, l.Title, l.Lead, l.Action = mailview.ArtSearch, lead, say("cancelled.title"), lead, view
		if !p.ToSeller {
			l.Note = say("cancelled.note")
			l.Action = &mailview.Action{Label: say("cancelled.action"), Path: "/search?q=" + url.QueryEscape(p.Title)}
		}
	case TypeSuspended:
		l.Title = Text(n.Type, p, tag)
		l.Art, l.Subject, l.Lead, l.Note = mailview.ArtOffline, l.Title, say("suspended.lead"), say("suspended.note")
		l.Action = &mailview.Action{Label: say("suspended.action"), Path: "/support"}
	case TypeUnsuspended:
		l.Art, l.Title, l.Lead = mailview.ArtPosted, Text(n.Type, p, tag), say("unsuspended.lead")
		l.Subject, l.Action = l.Title, &mailview.Action{Label: say("unsuspended.action"), Path: "/"}
	case TypeOffer, TypeCounter, TypeAgreed, TypeDeclined, TypeAgreementCancelled, TypeOfferVoided, TypeListingLive,
		TypeListingRejected, TypeFollowedListing, TypeReview, TypeReviewReply, TypeWatchMatched, TypePriceDropped:
		// Never mailed.
	default:
	}
	l.Preheader = l.Lead
	return l
}
