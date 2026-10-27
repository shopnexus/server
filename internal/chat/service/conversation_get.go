package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/platform/page"
)

func (c *Chat) Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (page.Page[InboxItem], error) {
	rows, err := c.chats.Inbox(ctx, account, archived, p)
	if err != nil {
		return page.Page[InboxItem]{}, fmt.Errorf("read inbox: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].Other)
	}
	others, err := c.accounts.GetMany(ctx, ids)
	if err != nil {
		return page.Page[InboxItem]{}, fmt.Errorf("read the other members: %w", err)
	}
	items := make([]InboxItem, 0, len(rows))
	for i := range rows {
		other := others[rows[i].Other]
		items = append(items, InboxItem{
			ID: rows[i].Conversation.ID, Other: PersonView{ID: rows[i].Other, DisplayName: other.DisplayName, AvatarURL: other.AvatarURL},
			LastMessage: c.messageView(rows[i].Last), Unread: rows[i].Unread, Archived: archived,
		})
	}
	return page.Of(items, p, func(it InboxItem) uuid.UUID { return it.LastMessage.ID }), nil
}

func (c *Chat) Get(ctx context.Context, account, conversation uuid.UUID) (ConversationView, error) {
	conv, m, err := c.member(ctx, account, conversation)
	if err != nil {
		return ConversationView{}, err
	}
	id := conv.Other(account)
	other, err := c.accounts.Get(ctx, id)
	if err != nil {
		return ConversationView{}, fmt.Errorf("read the other member: %w", err)
	}
	return ConversationView{
		ID: conv.ID, Other: PersonView{ID: id, DisplayName: other.DisplayName, AvatarURL: other.AvatarURL},
		Archived: m.ArchivedAt != nil,
	}, nil
}

func (c *Chat) ConversationWith(ctx context.Context, account, other uuid.UUID) (uuid.UUID, error) {
	conv, err := c.chats.FindPair(ctx, account, other)
	if errors.Is(err, repository.ErrNotFound) {
		return uuid.UUID{}, nil
	}
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("find conversation: %w", err)
	}
	return conv.ID, nil
}

func (c *Chat) Messages(ctx context.Context, account, conversation uuid.UUID, p page.Request) (page.Page[MessageView], error) {
	if _, _, err := c.member(ctx, account, conversation); err != nil {
		return page.Page[MessageView]{}, err
	}
	rows, err := c.chats.Messages(ctx, conversation, account, p)
	if err != nil {
		return page.Page[MessageView]{}, fmt.Errorf("read messages: %w", err)
	}
	items := make([]MessageView, 0, len(rows))
	for i := range rows {
		items = append(items, c.messageView(rows[i]))
	}
	return page.Of(items, p, func(m MessageView) uuid.UUID { return m.ID }), nil
}

func (c *Chat) Unread(ctx context.Context, account uuid.UUID) (int, error) {
	n, err := c.chats.Unread(ctx, account)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}
