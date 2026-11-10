package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
)

// Send checks that the account may write and the text's length, then adds
// the feedback under the account's daily limit.
func (s *Support) Send(ctx context.Context, account, key uuid.UUID, body string) (FeedbackView, error) {
	status, err := s.accounts.Status(ctx, account)
	if err != nil {
		return FeedbackView{}, fmt.Errorf("read account status: %w", err)
	}
	if r := status.Refusal(); r != nil {
		return FeedbackView{}, r
	}
	b, err := domain.Body(body)
	if err != nil {
		return FeedbackView{}, err
	}
	now := s.now()
	r, err := s.feedback.Add(ctx, repository.Row{ID: uuid.NewV7(), AccountID: account, Body: b, CreatedAt: now}, key, now.Add(-24*time.Hour))
	if err != nil {
		return FeedbackView{}, fmt.Errorf("add feedback: %w", err)
	}
	return FeedbackView{ID: r.ID, Body: r.Body, CreatedAt: r.CreatedAt}, nil
}
