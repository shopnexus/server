package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/audit"
)

// Reply sets ShopNexus's reply; only the first tells the owner.
func (s *Feedback) Reply(ctx context.Context, id uuid.UUID, text string, by audit.Actor) (ConsoleFeedback, error) {
	text, err := domain.ReplyText(text)
	if err != nil {
		return ConsoleFeedback{}, err
	}
	f, err := s.d.Store.Reply(ctx, id, text, by, s.d.Now())
	if err != nil {
		return ConsoleFeedback{}, fmt.Errorf("reply: %w", err)
	}
	return s.console(ctx, f)
}
