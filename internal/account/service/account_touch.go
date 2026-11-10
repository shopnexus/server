package service

import (
	"context"
	"fmt"
	"uuid"
)

func (s *Account) Touch(ctx context.Context, id uuid.UUID) error {
	if err := s.accounts.TouchActive(ctx, id, s.now()); err != nil {
		return fmt.Errorf("touch: %w", err)
	}
	return nil
}
