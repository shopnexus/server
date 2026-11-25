package service

import (
	"context"
	"fmt"
	"uuid"
)

func (s *Feedback) AccountDeleted(ctx context.Context, account uuid.UUID) error {
	if err := s.d.Store.Forget(ctx, account); err != nil {
		return fmt.Errorf("delete the account's feedback: %w", err)
	}
	return nil
}
