package service

import (
	"context"
	"fmt"
	"uuid"

	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
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

	Deal             *dealsvc.DealSummary      `json:"deal,omitempty"`
	OtherActiveDeals int                       `json:"other_active_deals"`
	ScamWarning      *trustsvc.ScamWarningView `json:"scam_warning,omitempty"`
}

// Inbox reads a page of account's conversations, the deals of their pairs,
// account's scam warnings in them and account's unread count, one call
// each.
func (s *Screen) Inbox(ctx context.Context, account uuid.UUID, box chatdomain.Box, p page.Request, t locale.Tag) (InboxScreen, error) {
	conversations, err := s.d.Chats.Inbox(ctx, account, box, p)
	if err != nil {
		return InboxScreen{}, fmt.Errorf("read inbox: %w", err)
	}
	others := make([]uuid.UUID, 0, len(conversations.Items))
	ids := make([]uuid.UUID, 0, len(conversations.Items))
	for i := range conversations.Items {
		others = append(others, conversations.Items[i].Other.ID)
		ids = append(ids, conversations.Items[i].ID)
	}
	deals, err := s.d.Deals.PairSummaries(ctx, account, others)
	if err != nil {
		return InboxScreen{}, fmt.Errorf("read deals: %w", err)
	}
	warnings, err := s.d.Trust.ScamWarnings(ctx, account, ids, t)
	if err != nil {
		return InboxScreen{}, fmt.Errorf("read scam warnings: %w", err)
	}
	unread, err := s.d.Chats.Unread(ctx, account)
	if err != nil {
		return InboxScreen{}, fmt.Errorf("count unread: %w", err)
	}
	items := make([]InboxItem, 0, len(conversations.Items))
	for i := range conversations.Items {
		item := InboxItem{InboxItem: conversations.Items[i]}
		if d, ok := deals[item.Other.ID]; ok {
			item.Deal, item.OtherActiveDeals = &d.Deal, d.OtherActive
		}
		if w, ok := warnings[item.ID]; ok {
			item.ScamWarning = &w
		}
		items = append(items, item)
	}
	return InboxScreen{Items: items, NextCursor: conversations.NextCursor, Unread: unread}, nil
}
