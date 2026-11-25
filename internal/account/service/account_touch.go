package service

import (
	"context"
	"fmt"
	"uuid"
)

func (s *Account) Touch(ctx context.Context, ids []uuid.UUID) error {
	if err := s.d.Accounts.TouchActive(ctx, ids, s.d.Now()); err != nil {
		return fmt.Errorf("touch: %w", err)
	}
	return nil
}
