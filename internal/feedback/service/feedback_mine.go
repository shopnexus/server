package service

import (
	"context"
	"fmt"
	"uuid"
)

func (s *Feedback) Mine(ctx context.Context, account uuid.UUID) ([]FeedbackView, error) {
	fs, err := s.d.Store.OfAccount(ctx, account, MineLimit)
	if err != nil {
		return nil, fmt.Errorf("read feedback: %w", err)
	}
	out := make([]FeedbackView, 0, len(fs))
	for i := range fs {
		out = append(out, s.view(fs[i]))
	}
	return out, nil
}
