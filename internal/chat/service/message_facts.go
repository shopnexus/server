package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
)

// MessageFacts is a message as its push reads it.
type MessageFacts struct {
	ID, ConversationID, RecipientID uuid.UUID
	SenderName, Text                string
	Photos                          int
	// Kind is the conversation's: a support chat pushes as ShopNexus and
	// gets no scam check.
	Kind domain.ConversationKind
}

func (c *Chat) MessageFacts(ctx context.Context, id uuid.UUID) (MessageFacts, bool, error) {
	m, err := c.chats.GetMessage(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return MessageFacts{}, false, nil
	}
	if err != nil {
		return MessageFacts{}, false, fmt.Errorf("read message: %w", err)
	}
	conv, err := c.chats.Get(ctx, m.ConversationID())
	if err != nil {
		return MessageFacts{}, false, fmt.Errorf("read conversation: %w", err)
	}
	sender, err := c.accounts.Get(ctx, m.SenderID())
	if err != nil {
		return MessageFacts{}, false, fmt.Errorf("read sender: %w", err)
	}
	return MessageFacts{
		ID: id, ConversationID: m.ConversationID(), RecipientID: conv.Other(m.SenderID()),
		SenderName: sender.DisplayName, Text: m.Body(), Photos: len(m.Photos()), Kind: conv.Kind,
	}, true, nil
}
