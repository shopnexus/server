package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

// QuickReplies is UC-DEAL-01 A5.
func (s *Chat) QuickReplies(ctx context.Context, account uuid.UUID, t locale.Tag) (QuickReplies, error) {
	items, saved, err := s.d.QuickReplies.Get(ctx, account)
	if err != nil {
		return QuickReplies{}, fmt.Errorf("read quick replies: %w", err)
	}
	if !saved {
		items = domain.DefaultQuickReplies(t)
	}
	return QuickReplies{Items: items}, nil
}

// PutQuickReplies is UC-DEAL-01 A5.
func (s *Chat) PutQuickReplies(ctx context.Context, account uuid.UUID, items []string) (QuickReplies, error) {
	items = domain.TrimQuickReplies(items)
	if err := s.d.QuickReplies.Put(ctx, account, items); err != nil {
		return QuickReplies{}, fmt.Errorf("save quick replies: %w", err)
	}
	return QuickReplies{Items: items}, nil
}

func (s *Chat) DeleteAccount(ctx context.Context, account uuid.UUID) error {
	if err := s.d.QuickReplies.Delete(ctx, account); err != nil {
		return fmt.Errorf("delete quick replies: %w", err)
	}
	if err := s.d.Chats.DeleteReplySamples(ctx, account); err != nil {
		return fmt.Errorf("delete reply samples: %w", err)
	}
	return nil
}
