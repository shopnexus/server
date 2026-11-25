package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

func (s *Chat) QuickReplies(ctx context.Context, account uuid.UUID, t locale.Tag) (QuickReplies, error) {
	items, saved, err := s.replies.Get(ctx, account)
	if err != nil {
		return QuickReplies{}, fmt.Errorf("read quick replies: %w", err)
	}
	if !saved {
		items = domain.DefaultQuickReplies(t)
	}
	return QuickReplies{Items: items}, nil
}

func (s *Chat) PutQuickReplies(ctx context.Context, account uuid.UUID, items []string) (QuickReplies, error) {
	items = domain.TrimQuickReplies(items)
	if err := s.replies.Put(ctx, account, items); err != nil {
		return QuickReplies{}, fmt.Errorf("save quick replies: %w", err)
	}
	return QuickReplies{Items: items}, nil
}

func (s *Chat) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if err := s.replies.Delete(ctx, account); err != nil {
		return fmt.Errorf("delete quick replies: %w", err)
	}
	if err := s.chats.DeleteReplySamples(ctx, account); err != nil {
		return fmt.Errorf("delete reply samples: %w", err)
	}
	return nil
}
