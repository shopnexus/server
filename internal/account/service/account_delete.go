package service

import (
	"context"
	"fmt"
	"uuid"
)

// Delete removes an account; deleting a missing one does nothing.
func (s *Account) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.accounts.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}
