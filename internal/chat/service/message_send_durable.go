package service

import (
	"errors"
	"fmt"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// messageSend is the MessageSend service's name without the prefix.
const messageSend = "MessageSend"

// SendFlowInput is one send: into ConversationID, or, when Opening, into
// the pair of the sender and the seller of ListingID. Key is the sender's
// request key, which the message claims. ReplyTo is the message it quotes;
// OpensTicket lets a support ticket's first message carry no text.
type SendFlowInput struct {
	Sender         uuid.UUID   `json:"sender"`
	Key            uuid.UUID   `json:"key,omitzero"`
	ConversationID uuid.UUID   `json:"conversation_id"`
	Opening        bool        `json:"opening"`
	ListingID      *uuid.UUID  `json:"listing_id,omitempty"`
	Text           string      `json:"text"`
	Photos         []uuid.UUID `json:"photos"`
	ReplyTo        *uuid.UUID  `json:"reply_to,omitempty"`
	OpensTicket    bool        `json:"opens_ticket,omitempty"`
}

// MessageSendName is the MessageSend service's name without the prefix,
// for module support's flow, which sends a ticket's first message.
const MessageSendName = messageSend

// SendResult names the conversation and the message sent, or carries the
// refusal.
type SendResult struct {
	ConversationID uuid.UUID     `json:"conversation_id"`
	MessageID      uuid.UUID     `json:"message_id"`
	Refused        *apperr.Error `json:"refused,omitempty"`
}

func (r SendResult) Refusal() *apperr.Error { return r.Refused }

// facts is what the read step found for a send, for the flow to decide on.
type facts struct {
	Status       accountdomain.Status `json:"status"`
	HasPhone     bool                 `json:"has_phone"`
	Listing      *catalogsvc.Summary  `json:"listing,omitempty"`
	Conversation *domain.Conversation `json:"conversation,omitempty"`
	Blocked      bool                 `json:"blocked"`
	OtherDeleted bool                 `json:"other_deleted"`
	// ReplyOK is whether ReplyTo is a sent message of the conversation
	// that is not a deal message.
	ReplyOK bool `json:"reply_ok"`
}

// MessageSend sends a message: one step reads what the send depends on and
// the flow decides on it between steps; then it opens the pair's
// conversation from a listing, adds the message and attaches its photos,
// each a step, and starts MessagePublish for a photo message. An attach
// that refuses deletes the message again.
type MessageSend struct{ Chat *Chat }

func (s MessageSend) ServiceName() string { return s.Chat.d.Prefix + messageSend }

// Run is UC-DEAL-01, steps 2 and 3.
func (s MessageSend) Run(ctx restate.Context, in SendFlowInput) (SendResult, error) {
	at, err := restatex.Now(ctx, s.Chat.d.Now)
	if err != nil {
		return SendResult{}, err
	}
	newConversation := restatex.NewID(ctx, at)
	f, err := restate.Run(ctx, func(rc restate.RunContext) (facts, error) {
		var out facts
		var stepErr error
		if out.Status, stepErr = s.Chat.d.Accounts.Status(rc, in.Sender); stepErr != nil {
			return facts{}, fmt.Errorf("read account status: %w", stepErr)
		}
		if in.Opening {
			if out.HasPhone, stepErr = s.Chat.d.Accounts.HasPhone(rc, in.Sender); stepErr != nil {
				return facts{}, fmt.Errorf("read the sender's phone: %w", stepErr)
			}
		}
		if in.ListingID != nil {
			l, ok, readErr := s.Chat.d.Catalog.Summary(rc, *in.ListingID)
			if readErr != nil {
				return facts{}, fmt.Errorf("read listing: %w", readErr)
			}
			if ok {
				out.Listing = &l
			}
		}
		other := uuid.UUID{}
		if in.Opening {
			if out.Listing != nil {
				other = out.Listing.SellerID
			}
		} else {
			conv, getErr := s.Chat.d.Chats.Get(rc, in.ConversationID)
			if getErr != nil && !errors.Is(getErr, repository.ErrNotFound) {
				return facts{}, fmt.Errorf("get conversation: %w", getErr)
			}
			if getErr == nil && conv.Has(in.Sender) {
				out.Conversation, other = &conv, conv.Other(in.Sender)
			}
		}
		if in.ReplyTo != nil {
			q, getErr := s.Chat.d.Chats.GetMessage(rc, *in.ReplyTo)
			if getErr != nil && !errors.Is(getErr, repository.ErrNotFound) {
				return facts{}, fmt.Errorf("read the quoted message: %w", getErr)
			}
			out.ReplyOK = getErr == nil && q.ConversationID() == in.ConversationID && q.Status() == domain.StatusSent &&
				q.Deal() == nil
		}
		if other != (uuid.UUID{}) {
			if out.Blocked, stepErr = s.Chat.d.Accounts.Blocked(rc, in.Sender, other); stepErr != nil {
				return facts{}, fmt.Errorf("check block: %w", stepErr)
			}
			otherStatus, statusErr := s.Chat.d.Accounts.Status(rc, other)
			if statusErr != nil {
				return facts{}, fmt.Errorf("read the other's status: %w", statusErr)
			}
			out.OtherDeleted = otherStatus == accountdomain.StatusDeleted
		}
		return out, nil
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("read the send's facts: %w", err)
	}
	// A suspended person still writes to support, to ask why.
	toSupport := f.Conversation != nil && f.Conversation.Kind == domain.KindSupport
	var refused *apperr.Error
	switch {
	// E3
	case f.Status.Refusal() != nil && (f.Status != accountdomain.StatusSuspended || !toSupport):
		refused = f.Status.Refusal()
	// E2 · BR-ACC-03
	case in.Opening && !f.HasPhone:
		refused = accountdomain.ErrPhoneRequired
	case in.Opening && f.Listing == nil:
		refused = catalogdomain.ErrNotFound
	// E4
	case in.Opening && f.Listing.SellerID == in.Sender:
		refused = domain.ErrOwnListing
	// E5
	case in.Opening && f.Listing.Status != catalogdomain.StatusActive:
		refused = domain.ErrListingUnavailable
	case !in.Opening && f.Conversation == nil:
		refused = domain.ErrNotFound
	// E7
	case !in.Opening && in.ListingID != nil && (f.Listing == nil || !f.Conversation.Has(f.Listing.SellerID)):
		refused = domain.ErrInvalidMessage.WithField("listing_id", apperr.NotAllowed, "a listing of either member")
	case in.ReplyTo != nil && !f.ReplyOK:
		refused = domain.ErrInvalidMessage.WithField("reply_to", apperr.NotAllowed, "a sent message of this conversation, not a deal message")
	// E1 · BR-ACC-01
	case f.Blocked:
		refused = accountdomain.ErrBlocked
	// E6
	case f.OtherDeleted:
		refused = accountdomain.ErrAccountDeleted
	default:
	}
	if refused != nil {
		return SendResult{Refused: refused}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	conv := f.Conversation
	// 2.
	if in.Opening {
		opened, openErr := restate.Run(ctx, func(rc restate.RunContext) (domain.Conversation, error) {
			return s.Chat.d.Chats.OpenPair(rc, domain.NewConversation(newConversation, in.Sender, f.Listing.SellerID, at))
		})
		if openErr != nil {
			return SendResult{}, fmt.Errorf("open conversation: %w", openErr)
		}
		conv = &opened
	}
	msg, err := domain.NewMessage(restatex.NewID(ctx, at), conv.ID, in.Sender, in.Text, in.Photos, in.ListingID, in.ReplyTo,
		in.OpensTicket, at)
	if err != nil {
		return SendResult{Refused: apperr.From(err)}, nil
	}
	// 3. A2: a racing retry of this request may have sent its message: that
	// one is the answer, and its run attaches the photos (NFR-IDEM-01).
	added, err := restate.Run(ctx, func(rc restate.RunContext) (uuid.UUID, error) {
		return s.Chat.d.Chats.AddMessage(rc, msg, in.Key)
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("add message: %w", err)
	}
	if added != msg.ID() {
		return SendResult{ConversationID: conv.ID, MessageID: added}, nil
	}
	// A1
	if len(in.Photos) > 0 {
		attachWrite := restatex.NewID(ctx, at)
		attached, attachErr := restate.Run(ctx, func(rc restate.RunContext) (restatex.Refused, error) {
			return restatex.AsRefusal(s.Chat.d.Media.Attach(rc, in.Sender, msg.ID(), in.Photos, attachWrite))
		})
		if attachErr != nil {
			return SendResult{}, fmt.Errorf("attach photos: %w", attachErr)
		}
		// E8
		if attached.Refusal != nil {
			if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Chat.d.Chats.DeleteMessage(rc, msg.ID()) }); err != nil {
				return SendResult{}, fmt.Errorf("delete message: %w", err)
			}
			return SendResult{Refused: attached.Refusal}, nil
		}
		restate.ServiceSend(ctx, s.Chat.d.Prefix+messagePublish, "Run").
			Send(msg.ID(), restate.WithIdempotencyKey(msg.ID().String()))
	}
	return SendResult{ConversationID: conv.ID, MessageID: msg.ID()}, nil
}
