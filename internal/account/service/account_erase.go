package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/events"
)

// Erase deletes the account in one transaction with what goes with it,
// reading it again once when another write won the race.
func (s *Account) Erase(ctx context.Context, id uuid.UUID) error {
	for range 2 {
		now := s.now()
		a, err := s.load(ctx, id)
		if err != nil {
			return err
		}
		changed, err := a.Delete(now)
		if err != nil || !changed {
			return err
		}
		err = s.accounts.Erase(ctx, &a, uuid.NewV7(), events.Event{
			ID: uuid.NewV7(), Kind: events.AccountDeleted, SubjectID: id, OccurredAt: now,
		})
		if errors.Is(err, repository.ErrConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("erase account: %w", err)
		}
		return nil
	}
	return apperr.ErrTryAgain
}
