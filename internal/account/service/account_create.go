package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
)

// Create inserts an active account with the caller's id. Creating the same
// id twice does nothing, so a retried Restate step is safe.
func (s *Account) Create(ctx context.Context, id uuid.UUID, displayName string) error {
	a, err := domain.New(id, displayName, s.now())
	if err != nil {
		return err
	}
	if err := s.accounts.Create(ctx, a); err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	return nil
}
