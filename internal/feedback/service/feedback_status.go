package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/platform/audit"
)

// SetStatus moves id's whole group; each owner whose feedback moved to
// in_progress or shipped is told through notification.
func (s *Feedback) SetStatus(ctx context.Context, id uuid.UUID, in StatusInput, by audit.Actor) (ConsoleFeedback, error) {
	c, err := domain.NewStatusChange(in.Status, in.Note, in.Version)
	if err != nil {
		return ConsoleFeedback{}, err
	}
	f, err := s.d.Store.SetStatus(ctx, id, c, by, s.d.Now())
	if err != nil {
		return ConsoleFeedback{}, fmt.Errorf("set the status: %w", err)
	}
	return s.console(ctx, f)
}
