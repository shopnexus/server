package service

import (
	"context"
	"fmt"
	"slices"
	"uuid"

	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// ChatScreen is the ChatScreen schema in api/schemas/screen_inbox.yaml.
type ChatScreen struct {
	chatsvc.ConversationView

	// Blocked is a block between the two, either way.
	Blocked     bool                           `json:"blocked"`
	Deals       []dealsvc.DealView             `json:"deals"`
	Messages    page.Page[chatsvc.MessageView] `json:"messages"`
	ScamWarning *trustsvc.ScamWarningView      `json:"scam_warning,omitempty"`
	// Ticket is a support chat's ticket.
	Ticket *supportsvc.TicketView `json:"ticket,omitempty"`
	// BuyerRating is the other member's rating as a buyer, when the
	// caller sells to them in one of the pair's deals.
	BuyerRating *trustsvc.Rating `json:"buyer_rating,omitempty"`
}

// Chat reads a conversation of account's with the other member's read
// marker, whether a block stands between the two, the pair's deals, the
// first page of messages, account's scam warning in it and a support chat's
// ticket, and for a seller the other member's rating as a buyer; for an
// account outside the conversation it returns chat's
// ErrNotFound (conversation_not_found).
func (s *Screen) Chat(ctx context.Context, account, conversation uuid.UUID, t locale.Tag) (ChatScreen, error) {
	conv, err := s.d.Chats.Get(ctx, account, conversation)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read conversation: %w", err)
	}
	blocked, err := s.d.Accounts.Blocked(ctx, account, conv.Other.ID)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("check block: %w", err)
	}
	deals, err := s.d.Deals.Between(ctx, account, conv.Other.ID)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read deals: %w", err)
	}
	messages, err := s.d.Chats.Messages(ctx, account, conversation, page.Request{})
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read messages: %w", err)
	}
	warnings, err := s.d.Trust.ScamWarnings(ctx, account, []uuid.UUID{conversation}, t)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read the scam warning: %w", err)
	}
	ticket, isTicket, err := s.d.Support.OfConversation(ctx, account, conversation)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read the ticket: %w", err)
	}
	out := ChatScreen{ConversationView: conv, Blocked: blocked, Deals: deals, Messages: messages}
	if w, ok := warnings[conversation]; ok {
		out.ScamWarning = &w
	}
	if isTicket {
		out.Ticket = &ticket
	}
	if slices.ContainsFunc(deals, func(d dealsvc.DealView) bool { return d.Role == dealdomain.Seller }) {
		rating, ratingErr := s.d.Trust.Rating(ctx, conv.Other.ID, trustdomain.RoleBuyer)
		if ratingErr != nil {
			return ChatScreen{}, fmt.Errorf("read the buyer rating: %w", ratingErr)
		}
		out.BuyerRating = &rating
	}
	return out, nil
}
