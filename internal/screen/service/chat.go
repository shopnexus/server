package service

import (
	"context"
	"fmt"
	"uuid"

	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/page"
	supportsvc "github.com/shopnexus/server2/internal/support/service"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// ChatScreen is the ChatScreen schema in api/schemas/screen_inbox.yaml.
type ChatScreen struct {
	chatsvc.ConversationView

	Deals       []dealsvc.DealView             `json:"deals"`
	Messages    page.Page[chatsvc.MessageView] `json:"messages"`
	ScamWarning *trustsvc.ScamWarningView      `json:"scam_warning,omitempty"`
	// Ticket is a support chat's ticket.
	Ticket *supportsvc.TicketView `json:"ticket,omitempty"`
}

// Chat reads a conversation of account's, the pair's deals, the first page
// of messages, account's scam warning in it and a support chat's ticket; for an account outside the
// conversation it returns chat's ErrNotFound (conversation_not_found).
func (s *Screen) Chat(ctx context.Context, account, conversation uuid.UUID) (ChatScreen, error) {
	conv, err := s.chats.Get(ctx, account, conversation)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read conversation: %w", err)
	}
	deals, err := s.deals.Between(ctx, account, conv.Other.ID)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read deals: %w", err)
	}
	messages, err := s.chats.Messages(ctx, account, conversation, page.Request{})
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read messages: %w", err)
	}
	warnings, err := s.trust.ScamWarnings(ctx, account, []uuid.UUID{conversation})
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read the scam warning: %w", err)
	}
	ticket, isTicket, err := s.support.OfConversation(ctx, account, conversation)
	if err != nil {
		return ChatScreen{}, fmt.Errorf("read the ticket: %w", err)
	}
	out := ChatScreen{ConversationView: conv, Deals: deals, Messages: messages}
	if w, ok := warnings[conversation]; ok {
		out.ScamWarning = &w
	}
	if isTicket {
		out.Ticket = &ticket
	}
	return out, nil
}
