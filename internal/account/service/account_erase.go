package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/events"
)

// Erase deletes the account in one transaction with what goes with it,
// reading it again once when another write won the race. The ShopNexus
// account is refused.
//
// Erase is UC-ACC-09, step 3.
func (s *Account) Erase(ctx context.Context, id uuid.UUID) error {
	if id == domain.ShopNexusID {
		return domain.ErrSystemAccount
	}
	for range 2 {
		now := s.d.Now()
		a, err := s.load(ctx, id)
		if err != nil {
			return err
		}
		// E3. A suspension in force refuses the deletion.
		changed, err := a.Delete(now)
		if err != nil || !changed {
			return err
		}
		err = s.d.Accounts.Erase(ctx, &a, uuid.NewV7(), events.Event{
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
