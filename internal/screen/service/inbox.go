package service

import (
	"context"
	"fmt"
	"uuid"

	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

// InboxScreen is the InboxScreen schema in api/schemas/screen_inbox.yaml.
type InboxScreen struct {
	Items      []InboxItem `json:"items"`
	NextCursor *uuid.UUID  `json:"next_cursor,omitempty"`
	Unread     int         `json:"unread"`
}

// InboxItem is a conversation of the inbox with the pair's deal it is
// about.
type InboxItem struct {
	chatsvc.InboxItem

	Deal             *dealsvc.DealSummary `json:"deal,omitempty"`
	OtherActiveDeals int                  `json:"other_active_deals"`
}

// Inbox reads a page of account's conversations, the deals of their pairs
// and account's unread count, one call each.
func (s *Screen) Inbox(ctx context.Context, account uuid.UUID, archived bool, p page.Request) (InboxScreen, error) {
	conversations, err := s.chats.Inbox(ctx, account, archived, p)
	if err != nil {
		return InboxScreen{}, fmt.Errorf("read inbox: %w", err)
	}
	others := make([]uuid.UUID, 0, len(conversations.Items))
	for i := range conversations.Items {
		others = append(others, conversations.Items[i].Other.ID)
	}
	deals, err := s.deals.PairSummaries(ctx, account, others)
	if err != nil {
		return InboxScreen{}, fmt.Errorf("read deals: %w", err)
	}
	unread, err := s.chats.Unread(ctx, account)
	if err != nil {
		return InboxScreen{}, fmt.Errorf("count unread: %w", err)
	}
	items := make([]InboxItem, 0, len(conversations.Items))
	for i := range conversations.Items {
		item := InboxItem{InboxItem: conversations.Items[i]}
		if d, ok := deals[item.Other.ID]; ok {
			item.Deal, item.OtherActiveDeals = &d.Deal, d.OtherActive
		}
		items = append(items, item)
	}
	return InboxScreen{Items: items, NextCursor: conversations.NextCursor, Unread: unread}, nil
}
