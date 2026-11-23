package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/audit"
)

// SameAs joins id to other's group; id's own feedback is refused.
func (s *Feedback) SameAs(ctx context.Context, id, other uuid.UUID, by audit.Actor) (ConsoleFeedback, error) {
	if id == other {
		return ConsoleFeedback{}, domain.ErrInvalid.WithField("feedback_id", apperr.NotAllowed, "another feedback")
	}
	f, err := s.store.Join(ctx, id, other, by, s.now())
	if err != nil {
		return ConsoleFeedback{}, fmt.Errorf("join: %w", err)
	}
	return s.console(f), nil
}
