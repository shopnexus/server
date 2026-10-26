package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

func (c *Chat) Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (page.Page[InboxItem], error) {
	rows, err := c.chats.Inbox(ctx, account, archived, p)
	if err != nil {
		return page.Page[InboxItem]{}, fmt.Errorf("read inbox: %w", err)
	}
	items := make([]InboxItem, 0, len(rows))
	for i := range rows {
		other, personErr := c.person(ctx, rows[i].Other)
		if personErr != nil {
			return page.Page[InboxItem]{}, personErr
		}
		items = append(items, InboxItem{
			ID: rows[i].Conversation.ID, Other: other, LastMessage: c.messageView(rows[i].Last),
			Unread: rows[i].Unread, Archived: archived,
		})
	}
	return page.Of(items, p, func(it InboxItem) uuid.UUID { return it.LastMessage.ID }), nil
}

// Get reads the conversation with the deals of its two members, as account
// sees them.
func (c *Chat) Get(ctx context.Context, account, conversation uuid.UUID) (ConversationView, error) {
	conv, m, err := c.member(ctx, account, conversation)
	if err != nil {
		return ConversationView{}, err
	}
	other, err := c.person(ctx, conv.Other(account))
	if err != nil {
		return ConversationView{}, err
	}
	deals, err := c.deals.Between(ctx, account, other.ID)
	if err != nil {
		return ConversationView{}, fmt.Errorf("read deals: %w", err)
	}
	return ConversationView{ID: conv.ID, Other: other, Deals: deals, Archived: m.ArchivedAt != nil}, nil
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
