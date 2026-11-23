package service

import (
	"context"
	"fmt"
	"uuid"

	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

// InboxSearchScreen is the InboxSearch schema in
// api/schemas/screen_inbox.yaml.
type InboxSearchScreen struct {
	Threads    []InboxItem          `json:"threads"`
	Messages   []chatsvc.MessageHit `json:"messages"`
	NextCursor *uuid.UUID           `json:"next_cursor,omitempty"`
}

// InboxSearch reads chat's search, the deals of the threads' pairs and the
// caller's scam warnings in the threads.
func (s *Screen) InboxSearch(ctx context.Context, account uuid.UUID, q string, p page.Request, t locale.Tag) (InboxSearchScreen, error) {
	found, err := s.chats.Search(ctx, account, q, p)
	if err != nil {
		return InboxSearchScreen{}, fmt.Errorf("search the inbox: %w", err)
	}
	others := make([]uuid.UUID, 0, len(found.Threads))
	ids := make([]uuid.UUID, 0, len(found.Threads))
	for i := range found.Threads {
		others = append(others, found.Threads[i].Other.ID)
		ids = append(ids, found.Threads[i].ID)
	}
	deals, err := s.deals.PairSummaries(ctx, account, others)
	if err != nil {
		return InboxSearchScreen{}, fmt.Errorf("read deals: %w", err)
	}
	warnings, err := s.trust.ScamWarnings(ctx, account, ids, t)
	if err != nil {
		return InboxSearchScreen{}, fmt.Errorf("read scam warnings: %w", err)
	}
	threads := make([]InboxItem, 0, len(found.Threads))
	for i := range found.Threads {
		item := InboxItem{InboxItem: found.Threads[i]}
		if d, ok := deals[item.Other.ID]; ok {
			item.Deal, item.OtherActiveDeals = &d.Deal, d.OtherActive
		}
		if w, ok := warnings[item.ID]; ok {
			item.ScamWarning = &w
		}
		threads = append(threads, item)
	}
	return InboxSearchScreen{Threads: threads, Messages: found.Messages.Items, NextCursor: found.Messages.NextCursor}, nil
}
