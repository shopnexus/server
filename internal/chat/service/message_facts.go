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
	// Ignored is whether the recipient ignores the conversation: nothing
	// pushes.
	Ignored bool
}

func (c *Chat) MessageFacts(ctx context.Context, id uuid.UUID) (MessageFacts, bool, error) {
	m, err := c.d.Chats.GetMessage(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return MessageFacts{}, false, nil
	}
	if err != nil {
		return MessageFacts{}, false, fmt.Errorf("read message: %w", err)
	}
	conv, err := c.d.Chats.Get(ctx, m.ConversationID())
	if err != nil {
		return MessageFacts{}, false, fmt.Errorf("read conversation: %w", err)
	}
	sender, err := c.d.Accounts.Get(ctx, m.SenderID())
	if err != nil {
		return MessageFacts{}, false, fmt.Errorf("read sender: %w", err)
	}
	members, err := c.d.Chats.Members(ctx, conv.ID)
	if err != nil {
		return MessageFacts{}, false, fmt.Errorf("read members: %w", err)
	}
	recipient := conv.Other(m.SenderID())
	return MessageFacts{
		ID: id, ConversationID: m.ConversationID(), RecipientID: recipient,
		SenderName: sender.DisplayName, Text: m.Body(), Photos: len(m.Photos()), Kind: conv.Kind,
		Ignored: members[recipient].IgnoredAt != nil,
	}, true, nil
}
