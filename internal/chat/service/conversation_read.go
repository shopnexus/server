package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
)

// Read moves account's marker to message, a message account may see in the
// conversation; an older one changes nothing. The other member's stream
// hears of a move.
func (c *Chat) Read(ctx context.Context, account, conversation, message uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	msg, err := c.chats.GetMessage(ctx, message)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && (msg.ConversationID() != conversation || !msg.VisibleTo(account))) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get message: %w", err)
	}
	if err = c.chats.MarkRead(ctx, conversation, account, message, conv.Other(account)); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

func (c *Chat) MarkUnread(ctx context.Context, account, conversation uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	if conv.Kind == domain.KindSupport {
		return domain.ErrSupportConversation
	}
	if err = c.chats.MarkUnread(ctx, conversation, account); err != nil {
		return fmt.Errorf("mark unread: %w", err)
	}
	return nil
}

func (c *Chat) Archive(ctx context.Context, account, conversation uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	if conv.Kind == domain.KindSupport {
		return domain.ErrSupportConversation
	}
	if err = c.chats.SetArchived(ctx, conversation, account, new(c.now())); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	return nil
}

func (c *Chat) Unarchive(ctx context.Context, account, conversation uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	if conv.Kind == domain.KindSupport {
		return domain.ErrSupportConversation
	}
	if err = c.chats.SetArchived(ctx, conversation, account, nil); err != nil {
		return fmt.Errorf("unarchive: %w", err)
	}
	return nil
}
