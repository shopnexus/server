// Package worker holds module notification's Restate handlers: one per
// event kind it tells people about.
package worker

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/provider/push"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// Worker turns events into notices and pushes. Mount subscribes each
// handler to the event kind it is named after.
type Worker struct {
	Prefix   string
	Notices  service.Service
	Deals    dealsvc.Service
	Chats    chatsvc.Service
	Catalog  catalogsvc.Service
	Trust    trustsvc.Service
	Accounts accountsvc.Service
	Auth     authsvc.Service
	Push     push.Sender
	Log      *slog.Logger
}

func (w Worker) ServiceName() string { return w.Prefix + "NotificationWorker" }

// DealMoved tells the other side of each move the event carries. A move the
// deal does not have, as when a replayed save minted other IDs than the
// ones it committed, tells no one.
func (w Worker) DealMoved(ctx restate.Context, e events.Event) error {
	var ids []uuid.UUID
	raw, _ := e.Payload["moves"].([]any) //nolint:revive // an event without moves tells no one
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if id, err := uuid.Parse(s); err == nil {
			ids = append(ids, id)
		}
	}
	f, err := restate.Run(ctx, func(rc restate.RunContext) (domain.DealFacts, error) {
		facts, found, stepErr := w.Deals.Moves(rc, e.SubjectID, ids)
		if stepErr != nil {
			return domain.DealFacts{}, fmt.Errorf("read moves: %w", stepErr)
		}
		if !found {
			return domain.DealFacts{}, nil
		}
		out := domain.DealFacts{
			DealID: facts.DealID, ListingID: facts.ListingID, BuyerID: facts.BuyerID, SellerID: facts.SellerID,
			Title: facts.ListingTitle, BuyerName: facts.BuyerName, SellerName: facts.SellerName,
		}
		for _, m := range facts.Moves {
			out.Moves = append(out.Moves, domain.Move{Kind: string(m.Kind), By: string(m.By), Amount: m.Amount})
		}
		if o := facts.Order; o != nil {
			out.CancelReason = string(o.CancelReason)
			if o.Waybill != nil {
				out.Carrier, out.TrackingCode = string(o.Waybill.Carrier), o.Waybill.TrackingCode
				if o.Waybill.CarrierName != "" {
					out.Carrier = o.Waybill.CarrierName
				}
			}
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("read the deal's moves: %w", err)
	}
	return w.tell(ctx, e, domain.FromMoves(f, e.OccurredAt))
}

// listingFacts is a listing as its notice reads it.
type listingFacts struct {
	Seller uuid.UUID `json:"seller"`
	Title  string    `json:"title"`
	Found  bool      `json:"found"`
}

// ListingPublished tells the seller the listing is on sale.
func (w Worker) ListingPublished(ctx restate.Context, e events.Event) error {
	return w.listing(ctx, e, true)
}

// ListingRejected tells the seller processing refused a photo.
func (w Worker) ListingRejected(ctx restate.Context, e events.Event) error {
	return w.listing(ctx, e, false)
}

func (w Worker) listing(ctx restate.Context, e events.Event, live bool) error {
	f, err := restate.Run(ctx, func(rc restate.RunContext) (listingFacts, error) {
		got, stepErr := w.Catalog.Summaries(rc, []uuid.UUID{e.SubjectID})
		if stepErr != nil {
			return listingFacts{}, fmt.Errorf("read listing: %w", stepErr)
		}
		s, ok := got[e.SubjectID]
		return listingFacts{Seller: s.SellerID, Title: s.Title, Found: ok}, nil
	})
	if err != nil {
		return fmt.Errorf("read the listing: %w", err)
	}
	if !f.Found {
		return nil
	}
	return w.tell(ctx, e, []domain.Notice{domain.ListingNotice(f.Seller, e.SubjectID, f.Title, live, e.OccurredAt)})
}

// ReviewPosted tells the seller a buyer reviewed them.
func (w Worker) ReviewPosted(ctx restate.Context, e events.Event) error {
	f, err := restate.Run(ctx, func(rc restate.RunContext) (trustsvc.ReviewFacts, error) {
		facts, _, stepErr := w.Trust.ReviewFacts(rc, e.SubjectID)
		if stepErr != nil {
			return trustsvc.ReviewFacts{}, fmt.Errorf("read review: %w", stepErr)
		}
		return facts, nil // an unknown review gives the zero facts
	})
	if err != nil {
		return fmt.Errorf("read the review: %w", err)
	}
	if f.ID == (uuid.UUID{}) {
		return nil
	}
	return w.tell(ctx, e, []domain.Notice{domain.Reviewed(f.SellerID, f.ID, f.BuyerName, f.ListingTitle, f.Stars, e.OccurredAt)})
}

// AccountSuspended tells the account it is suspended.
func (w Worker) AccountSuspended(ctx restate.Context, e events.Event) error {
	until, _ := e.Payload["until"].(string) //nolint:revive // no until is a suspension without end
	return w.tell(ctx, e, []domain.Notice{domain.Suspended(e.SubjectID, until, e.OccurredAt)})
}

// AccountUnsuspended tells the account it may write again.
func (w Worker) AccountUnsuspended(ctx restate.Context, e events.Event) error {
	return w.tell(ctx, e, []domain.Notice{domain.Unsuspended(e.SubjectID, e.OccurredAt)})
}

// MessageSent pushes a chat message to its recipient; the inbox lists it,
// so no notice is written.
func (w Worker) MessageSent(ctx restate.Context, e events.Event) error {
	f, err := restate.Run(ctx, func(rc restate.RunContext) (chatsvc.MessageFacts, error) {
		facts, _, stepErr := w.Chats.MessageFacts(rc, e.SubjectID)
		if stepErr != nil {
			return chatsvc.MessageFacts{}, fmt.Errorf("read message: %w", stepErr)
		}
		return facts, nil // an unknown message gives the zero facts
	})
	if err != nil {
		return fmt.Errorf("read the message: %w", err)
	}
	if f.ID == (uuid.UUID{}) {
		return nil
	}
	return w.deliver(ctx, f.RecipientID, push.Message{
		Title: "Tin nhắn", Body: domain.ChatBody(f.SenderName, f.Text, f.Photos), Tag: "conversation:" + f.ConversationID.String(),
		Data: map[string]string{"type": "message", "target_kind": "conversation", "target_id": f.ConversationID.String()},
	})
}

// tell writes each notice, then pushes it to its recipient.
func (w Worker) tell(ctx restate.Context, e events.Event, notices []domain.Notice) error {
	for _, n := range notices {
		id, err := restate.Run(ctx, func(rc restate.RunContext) (uuid.UUID, error) {
			return w.Notices.Record(rc, e.ID, n)
		})
		if err != nil {
			return fmt.Errorf("write a notice: %w", err)
		}
		tag := n.CollapseKey
		if tag == "" {
			tag = "notification:" + id.String()
		}
		target := domain.TargetOf(n.Type, n.Params)
		if pushErr := w.deliver(ctx, n.Recipient, push.Message{
			Title: domain.Title(n.Type), Body: domain.Text(n.Type, n.Params), Tag: tag,
			Data: map[string]string{
				"type": string(n.Type), "target_kind": target.Kind, "target_id": target.ID, "notification_id": id.String(),
			},
		}); pushErr != nil {
			return pushErr
		}
	}
	return nil
}

// deliver pushes m to every token of account whose session is still
// active, one step per token, so a retried send repeats no other. The
// tokens of ended sessions, and those FCM reports dead, are deleted.
func (w Worker) deliver(ctx restate.Context, account uuid.UUID, m push.Message) error {
	tokens, err := restate.Run(ctx, func(rc restate.RunContext) ([]service.Token, error) {
		all, stepErr := w.Notices.Tokens(rc, account)
		if stepErr != nil {
			return nil, fmt.Errorf("read tokens: %w", stepErr)
		}
		if len(all) == 0 {
			return nil, nil
		}
		sessions := make([]uuid.UUID, len(all))
		for i, t := range all {
			sessions[i] = t.Session
		}
		active, stepErr := w.Auth.ActiveSessions(rc, sessions)
		if stepErr != nil {
			return nil, fmt.Errorf("read sessions: %w", stepErr)
		}
		var out []service.Token
		for _, t := range all {
			if active[t.Session] {
				out = append(out, t)
				continue
			}
			if stepErr = w.Notices.ForgetToken(rc, t.Session); stepErr != nil {
				return nil, fmt.Errorf("delete an ended session's token: %w", stepErr)
			}
		}
		return out, nil
	})
	if err != nil {
		return fmt.Errorf("read push tokens: %w", err)
	}
	for _, t := range tokens {
		if sendErr := restate.RunVoid(ctx, func(rc restate.RunContext) error {
			err := w.Push.Send(rc, t.Token, m)
			switch {
			case err == nil:
				return nil
			case errors.Is(err, push.ErrDeadToken):
				if err = w.Notices.ForgetToken(rc, t.Session); err != nil {
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
