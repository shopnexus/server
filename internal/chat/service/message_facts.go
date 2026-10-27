package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/repository"
)

// pushRunes is how much of a message a push shows.
const pushRunes = 100

// MessageFacts is a message as its push reads it.
type MessageFacts struct {
	ID, ConversationID, SenderID, RecipientID uuid.UUID
	SenderName                                string
	Text                                      string // the first pushRunes runes of the body
	Photos                                    int
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
	text := []rune(m.Body())
	if len(text) > pushRunes {
		text = text[:pushRunes]
	}
	return MessageFacts{
		ID: id, ConversationID: m.ConversationID(), SenderID: m.SenderID(), RecipientID: conv.Other(m.SenderID()),
		SenderName: sender.DisplayName, Text: string(text), Photos: len(m.Photos()),
	}, true, nil
}
