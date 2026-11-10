package service

import (
	"context"
	"fmt"
	"uuid"

	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

// InboxSearchScreen is the InboxSearch schema in
// api/schemas/screen_inbox.yaml.
type InboxSearchScreen struct {
	Threads    []InboxItem          `json:"threads"`
	Messages   []chatsvc.MessageHit `json:"messages"`
	NextCursor *uuid.UUID           `json:"next_cursor,omitempty"`
}

// InboxSearch reads chat's search and the deals of the threads' pairs.
func (s *Screen) InboxSearch(ctx context.Context, account uuid.UUID, q string, p page.Request) (InboxSearchScreen, error) {
	found, err := s.chats.Search(ctx, account, q, p)
	if err != nil {
		return InboxSearchScreen{}, fmt.Errorf("search the inbox: %w", err)
	}
	others := make([]uuid.UUID, 0, len(found.Threads))
	for i := range found.Threads {
		others = append(others, found.Threads[i].Other.ID)
	}
	deals, err := s.deals.PairSummaries(ctx, account, others)
	if err != nil {
		return InboxSearchScreen{}, fmt.Errorf("read deals: %w", err)
	}
	threads := make([]InboxItem, 0, len(found.Threads))
	for i := range found.Threads {
		item := InboxItem{InboxItem: found.Threads[i]}
		if d, ok := deals[item.Other.ID]; ok {
			item.Deal, item.OtherActiveDeals = &d.Deal, d.OtherActive
		}
		threads = append(threads, item)
	}
	return InboxSearchScreen{Threads: threads, Messages: found.Messages.Items, NextCursor: found.Messages.NextCursor}, nil
}
