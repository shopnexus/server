package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

func (s *Feedback) Queue(ctx context.Context, status *domain.Status, p page.Request) (page.Page[ConsoleFeedback], error) {
	fs, err := s.store.Queue(ctx, status, p)
	if err != nil {
		return page.Page[ConsoleFeedback]{}, fmt.Errorf("read feedback: %w", err)
	}
	out := make([]ConsoleFeedback, 0, len(fs))
	for i := range fs {
		out = append(out, s.console(fs[i]))
	}
	return page.Of(out, p, func(c ConsoleFeedback) uuid.UUID { return c.Feedback.ID }), nil
}
