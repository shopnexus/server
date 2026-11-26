package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/audit"
)

// Reply sets ShopNexus's reply; only the first tells the owner. It is
// UC-MOD-11 step 4.
func (s *Feedback) Reply(ctx context.Context, id uuid.UUID, text string, by audit.Actor) (ConsoleFeedback, error) {
	text, err := domain.ReplyText(text)
	if err != nil {
		return ConsoleFeedback{}, err
	}
	// A2: a second reply replaces the text and tells no one.
	f, err := s.d.Store.Reply(ctx, id, text, by, s.d.Now())
	if err != nil {
		return ConsoleFeedback{}, fmt.Errorf("reply: %w", err)
	}
	return s.console(ctx, f)
}
