package service

import (
	"errors"
	"fmt"
	"slices"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	authdomain "github.com/shopnexus/server2/internal/auth/domain"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// messageSend is the MessageSend service's name without the prefix.
const messageSend = "MessageSend"

// SendFlowInput is one send: into ConversationID, or, when Opening, into
// the pair of the sender and the seller of ListingID.
type SendFlowInput struct {
	Sender         uuid.UUID   `json:"sender"`
	ConversationID uuid.UUID   `json:"conversation_id"`
	Opening        bool        `json:"opening"`
	ListingID      *uuid.UUID  `json:"listing_id,omitempty"`
	Text           string      `json:"text"`
	Photos         []uuid.UUID `json:"photos"`
}

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
	Suspended    bool                 `json:"suspended"`
	HasPhone     bool                 `json:"has_phone"`
	Listing      *catalogsvc.Summary  `json:"listing,omitempty"`
	Conversation *domain.Conversation `json:"conversation,omitempty"`
	Blocked      bool                 `json:"blocked"`
}

// refusal is what a writing step refused with, or none.
type refusal struct {
	Refused *apperr.Error `json:"refused,omitempty"`
}

// MessageSend sends a message: one step reads what the send depends on and
// the flow decides on it between steps; then it opens the pair's
// conversation from a listing, adds the message and attaches its photos,
// each a step, and starts MessagePublish for a photo message. An attach
// that refuses deletes the message again.
type MessageSend struct{ Chat *Chat }

func (s MessageSend) ServiceName() string { return s.Chat.prefix + messageSend }

func (s MessageSend) Run(ctx restate.Context, in SendFlowInput) (SendResult, error) {
	at, err := restatex.Now(ctx, s.Chat.now)
	if err != nil {
		return SendResult{}, err
	}
	newConversation := restatex.NewID(ctx, at)
	f, err := restate.Run(ctx, func(rc restate.RunContext) (facts, error) {
		var out facts
		st, stepErr := s.Chat.accounts.Status(rc, in.Sender)
		if stepErr != nil {
			return facts{}, fmt.Errorf("read account status: %w", stepErr)
		}
		out.Suspended = st == accountdomain.StatusSuspended
		if in.ListingID != nil {
			got, readErr := s.Chat.catalog.Summaries(rc, []uuid.UUID{*in.ListingID})
			if readErr != nil {
				return facts{}, fmt.Errorf("read listing: %w", readErr)
			}
			if l, ok := got[*in.ListingID]; ok {
				out.Listing = &l
			}
		}
		other := uuid.UUID{}
		if in.Opening {
			ids, readErr := s.Chat.auth.Identities(rc, in.Sender)
			if readErr != nil {
				return facts{}, fmt.Errorf("read identities: %w", readErr)
			}
			out.HasPhone = slices.ContainsFunc(ids, func(i authsvc.Identity) bool { return i.Kind == authdomain.KindPhone })
			if out.Listing != nil {
				other = out.Listing.SellerID
			}
		} else {
			conv, getErr := s.Chat.chats.Get(rc, in.ConversationID)
			if getErr != nil && !errors.Is(getErr, repository.ErrNotFound) {
				return facts{}, fmt.Errorf("get conversation: %w", getErr)
			}
			if getErr == nil && conv.Has(in.Sender) {
				out.Conversation, other = &conv, conv.Other(in.Sender)
			}
		}
		if other != (uuid.UUID{}) {
			if out.Blocked, stepErr = s.Chat.accounts.Blocked(rc, in.Sender, other); stepErr != nil {
				return facts{}, fmt.Errorf("check block: %w", stepErr)
			}
		}
		return out, nil
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("read the send's facts: %w", err)
	}
	var refused *apperr.Error
	switch {
	case f.Suspended:
		refused = accountdomain.ErrSuspended
	case in.Opening && f.Listing == nil:
		refused = catalogdomain.ErrNotFound
	case in.Opening && !f.HasPhone:
		refused = domain.ErrPhoneRequired
	case in.Opening && f.Listing.SellerID == in.Sender:
		refused = domain.ErrOwnListing
	case in.Opening && f.Listing.Status != catalogdomain.StatusActive:
		refused = domain.ErrListingUnavailable
	case !in.Opening && f.Conversation == nil:
		refused = domain.ErrNotFound
	case !in.Opening && in.ListingID != nil && (f.Listing == nil || !f.Conversation.Has(f.Listing.SellerID)):
		refused = domain.ErrInvalidMessage.WithField("listing_id", "a listing of either member")
	case f.Blocked:
		refused = accountdomain.ErrBlocked
	default:
	}
	if refused != nil {
		return SendResult{Refused: refused}, nil //nolint:nilerr // A refusal goes back as data; the flow did not fail.
	}
	conv := f.Conversation
	if in.Opening {
		opened, openErr := restate.Run(ctx, func(rc restate.RunContext) (domain.Conversation, error) {
			return s.Chat.chats.OpenPair(rc, domain.NewConversation(newConversation, in.Sender, f.Listing.SellerID, at))
		})
		if openErr != nil {
			return SendResult{}, fmt.Errorf("open conversation: %w", openErr)
		}
		conv = &opened
	}
	msg, err := domain.NewMessage(restatex.NewID(ctx, at), conv.ID, in.Sender, in.Text, in.Photos, in.ListingID, at)
	if err != nil {
		return SendResult{Refused: apperr.From(err)}, nil
	}
	if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Chat.chats.AddMessage(rc, msg) }); err != nil {
		return SendResult{}, fmt.Errorf("add message: %w", err)
	}
	if len(in.Photos) > 0 {
		attached, attachErr := restate.Run(ctx, func(rc restate.RunContext) (refusal, error) {
			stepErr := s.Chat.media.Attach(rc, in.Sender, msg.ID(), in.Photos)
			if e, ok := errors.AsType[*apperr.Error](stepErr); ok && e.Kind != apperr.KindInternal {
				return refusal{Refused: e}, nil
			}
			if stepErr != nil {
				return refusal{}, fmt.Errorf("attach: %w", stepErr)
			}
			return refusal{}, nil
		})
		if attachErr != nil {
			return SendResult{}, fmt.Errorf("attach photos: %w", attachErr)
		}
		if attached.Refused != nil {
			if err = restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Chat.chats.DeleteMessage(rc, msg.ID()) }); err != nil {
				return SendResult{}, fmt.Errorf("delete message: %w", err)
			}
			return SendResult{Refused: attached.Refused}, nil
		}
		restate.ServiceSend(ctx, s.Chat.prefix+messagePublish, "Run").
			Send(msg.ID(), restate.WithIdempotencyKey(msg.ID().String()))
	}
	return SendResult{ConversationID: conv.ID, MessageID: msg.ID()}, nil
}
