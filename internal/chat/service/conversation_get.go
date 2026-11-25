package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/platform/page"
)

func (c *Chat) Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (page.Page[InboxItem], error) {
	rows, err := c.d.Chats.Inbox(ctx, account, archived, p)
	if err != nil {
		return page.Page[InboxItem]{}, fmt.Errorf("read inbox: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].Other)
	}
	others, err := c.d.Accounts.GetMany(ctx, ids)
	if err != nil {
		return page.Page[InboxItem]{}, fmt.Errorf("read the other members: %w", err)
	}
	items := make([]InboxItem, 0, len(rows))
	for i := range rows {
		items = append(items, InboxItem{
			ID: rows[i].Conversation.ID, Other: PersonFromAccount(rows[i].Other, others[rows[i].Other]),
			LastMessage: c.messageView(rows[i].Last), Unread: rows[i].Unread, Archived: rows[i].Archived,
		})
	}
	return page.Of(items, p, func(it InboxItem) uuid.UUID { return it.LastMessage.ID }), nil
}

// Get reads both member rows in one query, so the other member's read
// marker costs no round trip of its own.
func (c *Chat) Get(ctx context.Context, account, conversation uuid.UUID) (ConversationView, error) {
	conv, err := c.member(ctx, account, conversation)
	if err != nil {
		return ConversationView{}, err
	}
	members, err := c.d.Chats.Members(ctx, conversation)
	if err != nil {
		return ConversationView{}, fmt.Errorf("get members: %w", err)
	}
	id := conv.Other(account)
	other, err := c.d.Accounts.Get(ctx, id)
	if err != nil {
		return ConversationView{}, fmt.Errorf("read the other member: %w", err)
	}
	return ConversationView{
		ID: conv.ID, Other: PersonFromAccount(id, other), Archived: members[account].ArchivedAt != nil,
		OtherRead: members[id].LastRead,
	}, nil
}

func (c *Chat) ConversationWith(ctx context.Context, account, other uuid.UUID) (uuid.UUID, error) {
	conv, err := c.d.Chats.FindPair(ctx, account, other)
	if errors.Is(err, repository.ErrNotFound) {
		return uuid.UUID{}, nil
	}
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("find conversation: %w", err)
	}
	return conv.ID, nil
}

func (c *Chat) Messages(ctx context.Context, account, conversation uuid.UUID, p page.Request) (page.Page[MessageView], error) {
	if _, err := c.member(ctx, account, conversation); err != nil {
		return page.Page[MessageView]{}, err
	}
	rows, err := c.d.Chats.Messages(ctx, conversation, account, p)
	if err != nil {
		return page.Page[MessageView]{}, fmt.Errorf("read messages: %w", err)
	}
	items := make([]MessageView, 0, len(rows))
	for i := range rows {
		items = append(items, c.messageView(rows[i]))
	}
	if err = c.decorate(ctx, account, items, rows); err != nil {
		return page.Page[MessageView]{}, err
	}
	return page.Of(items, p, func(m MessageView) uuid.UUID { return m.ID }), nil
}

func (c *Chat) Unread(ctx context.Context, account uuid.UUID) (int, error) {
	n, err := c.d.Chats.Unread(ctx, account)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

func (c *Chat) Peer(ctx context.Context, account, conversation uuid.UUID) (uuid.UUID, bool, error) {
	conv, err := c.d.Chats.Get(ctx, conversation)
	if errors.Is(err, repository.ErrNotFound) {
		return uuid.UUID{}, false, nil
	}
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("get conversation: %w", err)
	}
	if !conv.Has(account) {
		return uuid.UUID{}, false, nil
	}
	other := conv.Other(account)
	blocked, err := c.d.Accounts.Blocked(ctx, account, other)
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("check block: %w", err)
	}
	if blocked {
		return uuid.UUID{}, false, nil
	}
	return other, true, nil
}
