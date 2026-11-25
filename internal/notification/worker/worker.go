// Package worker holds module notification's Restate handlers: one per
// event kind it tells people about.
package worker

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	feedbacksvc "github.com/shopnexus/server/internal/feedback/service"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/mailview"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/provider/email"
	"github.com/shopnexus/server/internal/provider/push"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// Worker turns events into notices and pushes. Mount subscribes each
// handler to the event kind it is named after. A handler reads its facts in
// one step, which returns the zero facts for a subject that is gone; a zero
// ID then tells no one. A first publication sends each of the seller's
// followers to TellFollower, one invocation per follower; ListingPublished
// journals one send per follower. The recipient's settings may hold back
// a push or a mail.
type Worker struct {
	Prefix   string
	Notices  service.Service
	Deals    dealsvc.Service
	Chats    chatsvc.Service
	Catalog  catalogsvc.Service
	Trust    trustsvc.Service
	Accounts accountsvc.Service
	// Discovery gives the facts of search watches and price drops.
	Discovery discoverysvc.Service
	// Support gives the ticket of a support chat, which the chat's push
	// opens.
	Support supportsvc.Service
	// Feedback gives the facts of a feedback whose status moved or that
	// ShopNexus replied to.
	Feedback feedbacksvc.Service
	Push     push.Sender
	// Mail sends the notices the recipient's settings mail.
	Mail mailview.Mailer
	Log  *slog.Logger
	// FollowerPage is how many followers one step reads when a listing
	// first goes on sale.
	FollowerPage int
	// Now is the clock quiet hours are read on.
	Now func() time.Time
}

func (w Worker) ServiceName() string { return w.Prefix + "NotificationWorker" }

// DealMoved tells the other side of each move the event carries. A move the
// deal does not have, as when a replayed save minted other IDs than the
// ones it committed, tells no one.
func (w Worker) DealMoved(ctx restate.Context, e events.Event) error {
	ids := dealsvc.MovedIDs(e)
	f, err := read(ctx, "moves", func(rc restate.RunContext) (domain.DealFacts, bool, error) {
		facts, found, stepErr := w.Deals.Moves(rc, e.SubjectID, ids)
		if stepErr != nil || !found {
			return domain.DealFacts{}, found, stepErr //nolint:wrapcheck // read names the step in the error.
		}
		out := domain.DealFacts{
			DealID: facts.DealID, BuyerID: facts.BuyerID, SellerID: facts.SellerID,
			Title: facts.ListingTitle, BuyerName: facts.BuyerName, SellerName: facts.SellerName, Photo: facts.ListingPhoto,
		}
		if o := facts.Order; o != nil {
			out.Carrier, out.TrackingCode, out.CancelReason, _, _ = orderFacts(*o)
		}
		for _, m := range facts.Moves {
			mv := domain.Move{Kind: string(m.Kind), By: string(m.By), Amount: m.Amount}
			if m.OrderID != nil {
				mv.OrderID = *m.OrderID
				mv.Carrier, mv.TrackingCode, mv.CancelReason, mv.Price, mv.Fee = orderFacts(facts.Orders[*m.OrderID])
			}
			out.Moves = append(out.Moves, mv)
		}
		return out, true, nil
	})
	if err != nil {
		return err
	}
	if f.DealID == (uuid.UUID{}) {
		return nil
	}
	return w.tell(ctx, e.ID, domain.FromMoves(f, e.OccurredAt))
}

// orderFacts is what an order notice tells of o: its shipment's carrier,
// tracking code and fee, why it was cancelled, and its price.
func orderFacts(o dealdomain.Order) (carrier, code, reason string, price, fee int64) {
	if s := o.Shipment; s != nil {
		carrier, code = string(s.Carrier), s.TrackingCode
		if s.Fee != nil {
			fee = *s.Fee
		}
	}
	return carrier, code, string(o.CancelReason), o.Price, fee
}

// FollowerNotice is what TellFollower needs: the event the listing went on
// sale in, the follower to tell and the seller's name.
type FollowerNotice struct {
	EventID    uuid.UUID `json:"event_id"`
	Follower   uuid.UUID `json:"follower"`
	ListingID  uuid.UUID `json:"listing_id"`
	SellerName string    `json:"seller_name"`
	At         time.Time `json:"at"`
}

// ListingPublished tells the seller the listing is on sale. On its first
// publication it also sends each of the seller's followers, a page per
// step, to TellFollower; the idempotency key names the event and the
// follower, so a replay sends nobody a second invocation.
func (w Worker) ListingPublished(ctx restate.Context, e events.Event) error {
	l, err := w.listing(ctx, e, true)
	if err != nil {
		return err
	}
	first, _ := e.Payload["first"].(bool) //nolint:revive // an event without the flag is not a first publication
	if l.ID == (uuid.UUID{}) || !first {
		return nil
	}
	name, err := restate.Run(ctx, func(rc restate.RunContext) (string, error) {
		a, stepErr := w.Accounts.Get(rc, l.SellerID)
		if errors.Is(stepErr, accountdomain.ErrAccountNotFound) {
			return "", nil
		}
		if stepErr != nil {
			return "", fmt.Errorf("read seller: %w", stepErr)
		}
		return a.DisplayName, nil
	})
	if err != nil {
		return fmt.Errorf("read the seller: %w", err)
	}
	var cursor uuid.UUID
	for {
		p, err := restate.Run(ctx, func(rc restate.RunContext) (page.Page[accountsvc.Follower], error) {
			return w.Accounts.Followers(rc, l.SellerID, page.Request{Cursor: cursor, Limit: w.FollowerPage})
		})
		if err != nil {
			return fmt.Errorf("read the followers: %w", err)
		}
		for _, f := range p.Items {
			restate.ServiceSend(ctx, w.ServiceName(), "TellFollower").Send(
				FollowerNotice{EventID: e.ID, Follower: f.AccountID, ListingID: l.ID, SellerName: name, At: e.OccurredAt},
				restate.WithIdempotencyKey(e.ID.String()+"/"+f.AccountID.String()))
		}
		if p.NextCursor == nil {
			return nil
		}
		cursor = *p.NextCursor
	}
}

// ListingRejected tells the seller processing refused a photo.
func (w Worker) ListingRejected(ctx restate.Context, e events.Event) error {
	_, err := w.listing(ctx, e, false)
	return err
}

// listing tells the seller of the event's listing and returns the listing
// it read, zero for an unknown one; ListingPublished and ListingRejected
// call it.
func (w Worker) listing(ctx restate.Context, e events.Event, live bool) (catalogsvc.Summary, error) {
	f, err := read(ctx, "listing", func(rc restate.RunContext) (catalogsvc.Summary, bool, error) {
		return w.Catalog.Summary(rc, e.SubjectID)
	})
	if err != nil {
		return catalogsvc.Summary{}, err
	}
	if f.ID == (uuid.UUID{}) {
		return f, nil
	}
	return f, w.tell(ctx, e.ID, []domain.Notice{domain.ListingNotice(f.SellerID, f.ID, f.Title, live, e.OccurredAt)})
}

// TellFollower tells one follower of a listing that first went on sale,
// unless it is no longer on sale when this runs.
func (w Worker) TellFollower(ctx restate.Context, in FollowerNotice) error {
	l, err := read(ctx, "listing", func(rc restate.RunContext) (catalogsvc.Summary, bool, error) {
		return w.Catalog.Summary(rc, in.ListingID)
	})
	if err != nil {
		return err
	}
	if l.Status != catalogdomain.StatusActive {
		return nil
	}
	return w.tell(ctx, in.EventID, []domain.Notice{
		domain.FollowedListing(in.Follower, l.SellerID, in.SellerName, l.ID, l.Title, in.At),
	})
}

// ReviewPosted tells the subject of a review once it shows: at once, or
// after a durable sleep while it is hidden. A review revealed by the other
// side's is told by that review's handler, which tells both subjects.
func (w Worker) ReviewPosted(ctx restate.Context, e events.Event) error {
	f, err := w.reviewFacts(ctx, e.SubjectID)
	if err != nil {
		return err
	}
	at := e.OccurredAt
	if f.HiddenFor > 0 {
		if err = restate.Sleep(ctx, f.HiddenFor); err != nil {
			return fmt.Errorf("wait until the review shows: %w", err)
		}
		if f, err = w.reviewFacts(ctx, e.SubjectID); err != nil {
			return err
		}
		at = f.VisibleAt
	}
	if f.ID == (uuid.UUID{}) || f.RevealedBy != (uuid.UUID{}) {
		return nil
	}
	notices := []domain.Notice{domain.Reviewed(f.SubjectID, f.ReviewerName, f.ListingTitle, f.Stars, f.OrderID, at)}
	if f.Revealed != (uuid.UUID{}) {
		other, otherErr := w.reviewFacts(ctx, f.Revealed)
		if otherErr != nil {
			return otherErr
		}
		if other.ID != (uuid.UUID{}) {
			notices = append(notices, domain.Reviewed(other.SubjectID, other.ReviewerName, other.ListingTitle, other.Stars, other.OrderID, at))
		}
	}
	return w.tell(ctx, e.ID, notices)
}

// ReviewReplied tells a reviewer the subject replied.
func (w Worker) ReviewReplied(ctx restate.Context, e events.Event) error {
	f, err := w.reviewFacts(ctx, e.SubjectID)
	if err != nil || f.ID == (uuid.UUID{}) {
		return err
	}
	return w.tell(ctx, e.ID, []domain.Notice{domain.ReviewReplied(f.ReviewerID, f.SubjectName, f.ListingTitle, f.OrderID, e.OccurredAt)})
}

// reviewFacts reads a review's facts in one step; zero for a gone one.
func (w Worker) reviewFacts(ctx restate.Context, id uuid.UUID) (trustsvc.ReviewFacts, error) {
	return read(ctx, "review", func(rc restate.RunContext) (trustsvc.ReviewFacts, bool, error) {
		return w.Trust.ReviewFacts(rc, id)
	})
}

// SearchWatchMatched tells a watcher of the new listings a sweep found.
func (w Worker) SearchWatchMatched(ctx restate.Context, e events.Event) error {
	count, _ := e.Payload["count"].(float64) //nolint:revive // a payload without the count tells 0, which no sweep writes
	f, err := read(ctx, "watch", func(rc restate.RunContext) (discoverysvc.WatchFacts, bool, error) {
		return w.Discovery.WatchFacts(rc, e.SubjectID)
	})
	if err != nil || f.ID == (uuid.UUID{}) {
		return err
	}
	return w.tell(ctx, e.ID, []domain.Notice{domain.WatchMatched(f.Account, f.ID, f.Query, int(count), e.OccurredAt)})
}

// SavedPriceDropped tells a saver their saved listing fell to a new low.
// The event reaches the handler as JSON, so a payload number is a float64.
func (w Worker) SavedPriceDropped(ctx restate.Context, e events.Event) error {
	price, _ := e.Payload["price"].(float64) //nolint:revive // every drop carries its price
	f, err := read(ctx, "save", func(rc restate.RunContext) (discoverysvc.PriceDropFacts, bool, error) {
		return w.Discovery.PriceDropFacts(rc, e.SubjectID)
	})
	if err != nil || f.ID == (uuid.UUID{}) {
		return err
	}
	return w.tell(ctx, e.ID, []domain.Notice{
		domain.PriceDropped(f.Account, f.ListingID, f.Title, int64(price), f.SavedPrice, e.OccurredAt),
	})
}

// FeedbackMoved tells a feedback's owner that ShopNexus works on it or
// shipped it. A feedback gone, or moved on to another status since,
// tells no one.
func (w Worker) FeedbackMoved(ctx restate.Context, e events.Event) error {
	f, err := w.feedbackFacts(ctx, e.SubjectID)
	if err != nil || f.ID == (uuid.UUID{}) || !f.Status.Tells() {
		return err
	}
	return w.tell(ctx, e.ID, []domain.Notice{domain.FeedbackMoved(f.Owner, f.ID, string(f.Status), f.Version, e.OccurredAt)})
}

// FeedbackReplied tells a feedback's owner of ShopNexus's reply.
func (w Worker) FeedbackReplied(ctx restate.Context, e events.Event) error {
	f, err := w.feedbackFacts(ctx, e.SubjectID)
	if err != nil || f.ID == (uuid.UUID{}) || f.Reply == "" {
		return err
	}
	return w.tell(ctx, e.ID, []domain.Notice{domain.FeedbackReplied(f.Owner, f.ID, f.Reply, e.OccurredAt)})
}

// feedbackFacts reads a feedback's facts in one step; zero for a gone one.
func (w Worker) feedbackFacts(ctx restate.Context, id uuid.UUID) (feedbacksvc.Facts, error) {
	return read(ctx, "feedback", func(rc restate.RunContext) (feedbacksvc.Facts, bool, error) {
		return w.Feedback.Facts(rc, id)
	})
}

// AccountSuspended tells the account it is suspended.
func (w Worker) AccountSuspended(ctx restate.Context, e events.Event) error {
	until, _ := e.Payload["until"].(string) //nolint:revive // no until is a suspension without end
	return w.tell(ctx, e.ID, []domain.Notice{domain.Suspended(e.SubjectID, until, e.OccurredAt)})
}

// AccountDeleted deletes the deleted account's notices and push tokens. A
// notice of a later event about the account's deals can still be written;
// nobody reads it.
func (w Worker) AccountDeleted(ctx restate.Context, e events.Event) error {
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
		return w.Notices.Forget(rc, e.SubjectID)
	}); err != nil {
		return fmt.Errorf("forget %s: %w", e.SubjectID, err)
	}
	return nil
}

// AccountUnsuspended tells the account it may write again.
func (w Worker) AccountUnsuspended(ctx restate.Context, e events.Event) error {
	return w.tell(ctx, e.ID, []domain.Notice{domain.Unsuspended(e.SubjectID, e.OccurredAt)})
}

// MessageSent pushes a chat message to its recipient; the inbox lists it,
// so no notice is written.
func (w Worker) MessageSent(ctx restate.Context, e events.Event) error {
	f, err := read(ctx, "message", func(rc restate.RunContext) (chatsvc.MessageFacts, bool, error) {
		return w.Chats.MessageFacts(rc, e.SubjectID)
	})
	if err != nil || f.ID == (uuid.UUID{}) {
		return err
	}
	write := func(tag locale.Tag) push.Message {
		return push.Message{
			Title: domain.MessageTitle(tag), Body: domain.ChatBody(f.SenderName, f.Text, f.Photos, tag), Tag: "conversation:" + f.ConversationID.String(),
			Data: map[string]string{"type": "message", "target_kind": "conversation", "target_id": f.ConversationID.String()},
		}
	}
	if f.Kind == chatdomain.KindSupport {
		// A ticket's chat pushes only to the ticket's owner and opens the
		// ticket. ShopNexus is the title, so the body is the reply alone.
		// A message to ShopNexus pushes nothing: the console's queue shows it.
		ticket, stepErr := read(ctx, "ticket", func(rc restate.RunContext) (uuid.UUID, bool, error) {
			v, found, readErr := w.Support.OfConversation(rc, f.RecipientID, f.ConversationID)
			return v.ID, found, readErr //nolint:wrapcheck // read names the step in the error.
		})
		if stepErr != nil || ticket == (uuid.UUID{}) {
			return stepErr
		}
		write = func(tag locale.Tag) push.Message {
			return push.Message{
				Title: "ShopNexus", Body: domain.SupportBody(f.Text, f.Photos, tag), Tag: "conversation:" + f.ConversationID.String(),
				Data: map[string]string{"type": "support_message", "target_kind": "ticket", "target_id": ticket.String()},
			}
		}
	}
	return w.deliver(ctx, f.RecipientID, domain.SwitchMessages, write)
}

// read runs get in one step and returns its facts, zero for a subject that
// is gone (found false). what names the subject in a failure.
func read[T any](ctx restate.Context, what string, get func(restate.RunContext) (T, bool, error)) (T, error) {
	v, err := restate.Run(ctx, func(rc restate.RunContext) (T, error) {
		facts, found, stepErr := get(rc)
		var zero T
		switch {
		case stepErr != nil:
			return zero, fmt.Errorf("read %s: %w", what, stepErr)
		case !found:
			return zero, nil
		}
		return facts, nil
	})
	if err != nil {
		return v, fmt.Errorf("read the %s: %w", what, err)
	}
	return v, nil
}

// tell writes each notice, then pushes and mails it to its recipient. A
// notice a newer one with its key outran is neither written nor sent.
func (w Worker) tell(ctx restate.Context, event uuid.UUID, notices []domain.Notice) error {
	for i := range notices {
		n := &notices[i]
		// Minted in a step of its own, from the wall clock so notices with
		// one bump time list in write order, and a retried write keeps it.
		fresh, err := restate.Run(ctx, func(restate.RunContext) (uuid.UUID, error) { return uuid.NewV7(), nil })
		if err != nil {
			return fmt.Errorf("mint a notice ID: %w", err)
		}
		rec, err := restate.Run(ctx, func(rc restate.RunContext) (service.Recorded, error) {
			return w.Notices.Record(rc, event, fresh, *n)
		})
		if err != nil {
			return fmt.Errorf("write a notice: %w", err)
		}
		if rec.Late {
			continue
		}
		id := rec.ID
		// An unread notice keeps its id when a newer event with its
		// collapse key rewrites it, so this tag replaces that notice's
		// tray item, and the app removes the item by the notice's id when
		// the notice is read.
		tag := "notification:" + id.String()
		target := domain.TargetOf(n.Type, n.Params)
		if pushErr := w.deliver(ctx, n.Recipient, domain.SwitchOf(n.Type), func(lang locale.Tag) push.Message {
			return push.Message{
				Title: domain.Title(n.Type, lang), Body: domain.Text(n.Type, n.Params, lang), Tag: tag,
				Data: map[string]string{
					"type": string(n.Type), "target_kind": target.Kind, "target_id": target.ID, "notification_id": id.String(),
				},
			}
		}); pushErr != nil {
			return pushErr
		}
		// A type that is never mailed takes no step. A mailed one reads the
		// recipient's settings, address and language in one step and sends in
		// another, keyed so a retried send mails once; an account without
		// an address gets none, and a refused mail is dropped.
		if !domain.Mailed(n.Type) {
			continue
		}
		box, err := restate.Run(ctx, func(rc restate.RunContext) (accountsvc.Mailbox, error) {
			st, stepErr := w.Notices.Settings(rc, n.Recipient)
			if stepErr != nil {
				return accountsvc.Mailbox{}, fmt.Errorf("read settings: %w", stepErr)
			}
			if !st.Mails(n.Type) {
				return accountsvc.Mailbox{}, nil
			}
			b, stepErr := w.Accounts.Mailbox(rc, n.Recipient)
			if stepErr != nil {
				return accountsvc.Mailbox{}, fmt.Errorf("read the mailbox: %w", stepErr)
			}
			return b, nil
		})
		if err != nil {
			return fmt.Errorf("read the mail address: %w", err)
		}
		if box.Address == "" {
			continue
		}
		key := event.String() + "/" + n.Recipient.String() + "/" + strconv.Itoa(i)
		if err = restate.RunVoid(ctx, func(rc restate.RunContext) error {
			sendErr := w.Mail.Send(rc, box.Address, key, domain.Mail(*n, box.Locale))
			if errors.Is(sendErr, email.ErrRefused) {
				w.Log.WarnContext(rc, "mail refused", "type", n.Type, "err", sendErr)
				return nil
			}
			if sendErr != nil {
				return fmt.Errorf("send: %w", sendErr)
			}
			return nil
		}, restate.WithMaxRetryDuration(time.Hour)); err != nil {
			return fmt.Errorf("mail: %w", err)
		}
	}
	return nil
}

// audience is who a push goes to: the tokens of the account's active
// sessions, and the language the account reads.
type audience struct {
	Tokens []service.Token `json:"tokens"`
	Locale locale.Tag      `json:"locale"`
}

// deliver pushes write's message, in the account's language, to every token
// of account whose session is still active, one step per token, so a
// retried send repeats no other. A push the account's settings hold back,
// by sw or quiet hours, is dropped. The tokens of ended sessions, and those
// FCM reports dead, are deleted.
func (w Worker) deliver(ctx restate.Context, account uuid.UUID, sw domain.Switch, write func(locale.Tag) push.Message) error {
	to, err := restate.Run(ctx, func(rc restate.RunContext) (audience, error) {
		st, stepErr := w.Notices.Settings(rc, account)
		if stepErr != nil {
			return audience{}, fmt.Errorf("read settings: %w", stepErr)
		}
		if !st.Pushes(sw, w.Now()) {
			return audience{}, nil
		}
		all, stepErr := w.Notices.Tokens(rc, account)
		if stepErr != nil {
			return audience{}, fmt.Errorf("read tokens: %w", stepErr)
		}
		if len(all) == 0 {
			return audience{}, nil
		}
		sessions := make([]uuid.UUID, len(all))
		for i, t := range all {
			sessions[i] = t.Session
		}
		active, stepErr := w.Accounts.ActiveSessions(rc, sessions)
		if stepErr != nil {
			return audience{}, fmt.Errorf("read sessions: %w", stepErr)
		}
		var out audience
		for _, t := range all {
			if active[t.Session] {
				out.Tokens = append(out.Tokens, t)
				continue
			}
			if stepErr = w.Notices.DeleteToken(rc, t.Session); stepErr != nil {
				return audience{}, fmt.Errorf("delete an ended session's token: %w", stepErr)
			}
		}
		if len(out.Tokens) > 0 {
			if out.Locale, stepErr = w.Accounts.Locale(rc, account); stepErr != nil {
				return audience{}, fmt.Errorf("read the language: %w", stepErr)
			}
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("read push tokens: %w", err)
	}
	m := write(to.Locale)
	for _, t := range to.Tokens {
		if sendErr := restate.RunVoid(ctx, func(rc restate.RunContext) error {
			err := w.Push.Send(rc, t.Token, m)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, push.ErrDeadToken):
				if err = w.Notices.DeleteToken(rc, t.Session); err != nil {
					return fmt.Errorf("delete a dead token: %w", err)
				}
				return nil
			case errors.Is(err, push.ErrRetry):
				return fmt.Errorf("send: %w", err)
			default:
				w.Log.WarnContext(rc, "push dropped", "err", err)
				return nil
			}
		}, restate.WithMaxRetryDuration(time.Hour)); sendErr != nil {
			return fmt.Errorf("push: %w", sendErr)
		}
	}
	return nil
}
