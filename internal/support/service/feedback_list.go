package service

import (
	"context"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Feedback reads a page of feedback and its senders in one query each.
func (s *Support) Feedback(ctx context.Context, p page.Request) (page.Page[ConsoleFeedback], error) {
	rows, err := s.feedback.List(ctx, p)
	if err != nil {
		return page.Page[ConsoleFeedback]{}, fmt.Errorf("list feedback: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.AccountID)
	}
	people, err := s.accounts.GetMany(ctx, ids)
	if err != nil {
		return page.Page[ConsoleFeedback]{}, fmt.Errorf("read accounts: %w", err)
	}
	items := make([]ConsoleFeedback, 0, len(rows))
	for _, r := range rows {
		a := people[r.AccountID]
		items = append(items, ConsoleFeedback{
			ID: r.ID, Body: r.Body, CreatedAt: r.CreatedAt,
			Account: FeedbackAccount{ID: r.AccountID, DisplayName: a.DisplayName, Deleted: a.Status == accountdomain.StatusDeleted},
		})
	}
	return page.Of(items, p, func(f ConsoleFeedback) uuid.UUID { return f.ID }), nil
}
