package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
)

func (s *Feedback) Heart(ctx context.Context, id uuid.UUID, hearted bool, by audit.Actor) (ConsoleFeedback, error) {
	f, err := s.d.Store.Heart(ctx, id, hearted, by, s.d.Now())
	if err != nil {
		return ConsoleFeedback{}, fmt.Errorf("heart: %w", err)
	}
	return s.console(ctx, f)
}
