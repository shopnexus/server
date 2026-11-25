package domain

import (
	"net/url"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/mailview"
)

// Mail is the mail of notice n in language tag: the facts and the one action
// the recipient needs. Only the types Mailed reports are written here; any
// other is an empty letter. Its text is under mail.* in text/<tag>.json.
func Mail(n Notice, tag locale.Tag) mailview.Letter {
	w := writerFor(tag)
	p := n.Params
	actor := w.name(p.Actor)
	order := "/orders"
	if p.OrderID != (uuid.UUID{}) {
		order += "/" + p.OrderID.String()
	}
	viewOrder := &mailview.Action{Label: w.say("mail.action.view"), Path: order}

	l := mailview.Letter{
		Locale: w.tag,
		Art:    mailview.ArtVerified,
		Group:  w.say("title.account"),
		Why:    w.say("mail.why.account"),
	}
	// Every order mail shows the listing and links the switch that stops it.
	if types[n.Type].mail == mailOrders {
		l.Group = w.say("title.orders")
		l.Why = w.say("mail.why.orders")
		l.Footer = &mailview.Action{Label: w.say("mail.footer.settings"), Path: "/notifications#settings"}
		l.Item = &mailview.Item{Title: p.Title, Side: w.say("mail.side.seller", actor)}
		if p.ToSeller {
			l.Item.Side = w.say("mail.side.buyer", actor)
		}
		if o := n.Order; o != nil {
			l.Item.Photo = o.Photo
			if o.Price > 0 {
				l.Item.Price = locale.Money(w.tag, o.Price)
			}
		}
	}

	switch n.Type {
	case TypeCodRequested: // to the seller
		l.Art = mailview.ArtOrders
		l.Subject = w.say("mail.cod.subject", actor, p.Title)
		l.Title = w.say("mail.cod.title", actor)
		l.Lead = w.say("mail.cod.lead")
		l.Steps = w.orderSteps(1)
		l.Note = w.say("mail.money.seller")
		l.Action = &mailview.Action{Label: w.say("mail.cod.action"), Path: order}

	case TypeShipped: // to the buyer
		l.Art = mailview.ArtShipped
		l.Subject = w.say("mail.shipped.subject", actor, p.Title)
		l.Title = w.say("mail.shipped.title", actor)
		l.Lead = w.say("mail.shipped.lead")
		l.Steps = w.orderSteps(2)
		l.Rows = w.tracking(p)
		if o := n.Order; o != nil && o.Price > 0 && o.Fee > 0 {
			l.Rows = append(l.Rows, mailview.Row{Label: w.say("mail.row.pay"), Value: locale.Money(w.tag, o.Price+o.Fee), Strong: true})
		}
		l.Action = viewOrder

	case TypeCompleted:
		l.Steps = w.orderSteps(3)
		if p.ToSeller { // the buyer has it
			l.Art = mailview.ArtDeal
			l.Subject = w.say("mail.received.subject", actor, p.Title)
			l.Title = w.say("mail.received.title", actor)
			l.Lead = w.say("mail.received.lead")
			l.Note = w.say("mail.money.seller")
			l.Action = &mailview.Action{Label: w.say("mail.received.action"), Path: order}
			break
		}
		// To the buyer, asking for a review: the stars are the action.
		seller := w.name(p.Seller)
		l.Art = mailview.ArtReceived
		l.Subject = w.say("mail.delivered.subject", p.Title, seller)
		l.Title = w.say("mail.delivered.title")
		l.Lead = w.say("mail.delivered.lead", seller)
		l.Stars = order

	case TypeFailed:
		l.Art = mailview.ArtNotFound
		l.Subject = w.say("mail.failed.subject", p.Title)
		l.Title = w.say("mail.failed.title")
		l.Lead = w.say("mail.failed.lead")
		if p.ToSeller { // the parcel comes back
			l.Lead = w.say("mail.failed.lead.seller")
			l.Rows = w.tracking(p)
			l.Note = w.say("mail.failed.note.seller")
		}
		l.Action = viewOrder

	case TypeOrderCancelled:
		// The lead is the notice's own sentence, which names the reason.
		l.Art = mailview.ArtSearch
		l.Title = w.say("mail.cancelled.title")
		l.Lead = Text(n.Type, p, w.tag) + "."
		l.Subject = l.Lead
		l.Action = viewOrder
		if !p.ToSeller {
			l.Note = w.say("mail.cancelled.note")
			l.Action = &mailview.Action{Label: w.say("mail.cancelled.action"), Path: "/search?q=" + url.QueryEscape(p.Title)}
		}

	case TypeSuspended:
		l.Art = mailview.ArtOffline
		l.Title = Text(n.Type, p, w.tag)
		l.Subject = l.Title
		l.Lead = w.say("mail.suspended.lead")
		l.Note = w.say("mail.suspended.note")
		l.Action = &mailview.Action{Label: w.say("mail.suspended.action"), Path: "/support"}

	case TypeUnsuspended:
		l.Art = mailview.ArtPosted
		l.Title = Text(n.Type, p, w.tag)
		l.Subject = l.Title
		l.Lead = w.say("mail.unsuspended.lead")
		l.Action = &mailview.Action{Label: w.say("mail.unsuspended.action"), Path: "/"}

	case TypeOffer, TypeCounter, TypeAgreed, TypeDeclined, TypeAgreementCancelled, TypeOfferVoided, TypeListingLive,
		TypeListingRejected, TypeFollowedListing, TypeReview, TypeReviewReply, TypeWatchMatched, TypePriceDropped,
		TypeFeedbackMoved, TypeFeedbackReplied, TypeChototImported, TypeChototSoldHere:
		// Never mailed.
	default:
	}
	l.Preheader = l.Lead
	return l
}

// orderSteps are a COD order's four steps, at is the one under way.
func (w writer) orderSteps(at int) *mailview.Steps {
	return &mailview.Steps{
		Names: []string{w.say("mail.steps.agreed"), w.say("mail.steps.ordered"), w.say("mail.steps.received"), w.say("mail.steps.reviewed")},
		At:    at,
	}
}

// tracking is an order's carrier and tracking code as rows; none before it
// ships.
func (w writer) tracking(p Params) []mailview.Row {
	if p.Code == "" {
		return nil
	}
	return []mailview.Row{
		{Label: w.say("mail.row.carrier"), Value: carrierName(p.Carrier)},
		{Label: w.say("mail.row.code"), Value: p.Code, Mono: true},
	}
}
