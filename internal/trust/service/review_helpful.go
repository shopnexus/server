package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// MarkHelpful is UC-TRS-05 step 2. It records that the caller found a
// visible review helpful. Its reviewer and its subject get
// domain.ErrOwnReview; a hidden, deleted or unknown review is
// domain.ErrNotFound.
func (s *Trust) MarkHelpful(ctx context.Context, caller, id uuid.UUID) error {
	// E1 · BR-MOD-02
	status, err := s.d.Accounts.Status(ctx, caller)
	if err != nil {
		return fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return r
	}
	r, err := s.d.Reviews.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get review: %w", err)
	}
	now := s.d.Now()
	// E2
	if r.Deleted() || !r.Visible(now) {
		return domain.ErrNotFound
	}
	// E3
	if caller == r.ReviewerID() || caller == r.SubjectID() {
		return domain.ErrOwnReview
	}
	// 2. A second mark changes nothing.
	err = s.d.Reviews.MarkHelpful(ctx, id, caller, now)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("mark helpful: %w", err)
	}
	return nil
}

// UnmarkHelpful is UC-TRS-05 A1. It takes the caller's helpful mark back;
// without one it changes nothing.
func (s *Trust) UnmarkHelpful(ctx context.Context, caller, id uuid.UUID) error {
	// E1 · BR-MOD-02
	status, err := s.d.Accounts.Status(ctx, caller)
	if err != nil {
		return fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return r
	}
	if err = s.d.Reviews.UnmarkHelpful(ctx, id, caller); err != nil {
		return fmt.Errorf("unmark helpful: %w", err)
	}
	return nil
}
