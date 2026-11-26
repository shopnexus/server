package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
)

// Read is UC-DEAL-01, step 4: it moves account's marker to message, a
// message account may see in the conversation; an older one changes
// nothing. The other member's stream hears of a move.
func (c *Chat) Read(ctx context.Context, account, conversation, message uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	msg, err := c.d.Chats.GetMessage(ctx, message)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && (msg.ConversationID() != conversation || !msg.VisibleTo(account))) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get message: %w", err)
	}
	if err = c.d.Chats.MarkRead(ctx, conversation, account, message, conv.Other(account)); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}

// MarkUnread is UC-DEAL-01 A3.
func (c *Chat) MarkUnread(ctx context.Context, account, conversation uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	if conv.Kind == domain.KindSupport {
		return domain.ErrSupportConversation
	}
	if err = c.d.Chats.MarkUnread(ctx, conversation, account); err != nil {
		return fmt.Errorf("mark unread: %w", err)
	}
	return nil
}

// Archive is UC-DEAL-01 A3.
func (c *Chat) Archive(ctx context.Context, account, conversation uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	if conv.Kind == domain.KindSupport {
		return domain.ErrSupportConversation
	}
	if err = c.d.Chats.SetArchived(ctx, conversation, account, new(c.d.Now())); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	return nil
}

// Unarchive is UC-DEAL-01 A3.
func (c *Chat) Unarchive(ctx context.Context, account, conversation uuid.UUID) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	if conv.Kind == domain.KindSupport {
		return domain.ErrSupportConversation
	}
	if err = c.d.Chats.SetArchived(ctx, conversation, account, nil); err != nil {
		return fmt.Errorf("unarchive: %w", err)
	}
	return nil
}

// Ignore is UC-DEAL-01 A3.
func (c *Chat) Ignore(ctx context.Context, account, conversation uuid.UUID) error {
	return c.setIgnored(ctx, account, conversation, new(c.d.Now()))
}

// Unignore is UC-DEAL-01 A3.
func (c *Chat) Unignore(ctx context.Context, account, conversation uuid.UUID) error {
	return c.setIgnored(ctx, account, conversation, nil)
}

func (c *Chat) setIgnored(ctx context.Context, account, conversation uuid.UUID, at *time.Time) error {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return err
	}
	if conv.Kind == domain.KindSupport {
		return domain.ErrSupportConversation
	}
	if err = c.d.Chats.SetIgnored(ctx, conversation, account, at); err != nil {
		return fmt.Errorf("set ignored: %w", err)
	}
	return nil
}

func (c *Chat) Ignores(ctx context.Context, account, other uuid.UUID) (bool, error) {
	conv, err := c.d.Chats.FindPair(ctx, account, other)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find pair: %w", err)
	}
	members, err := c.d.Chats.Members(ctx, conv.ID)
	if err != nil {
		return false, fmt.Errorf("get members: %w", err)
	}
	return members[account].IgnoredAt != nil, nil
}
