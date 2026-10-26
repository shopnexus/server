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

// Rename sets the display name. It is a write, so a suspended account gets
// ErrSuspended even while its access token is still valid.
func (s *Account) Rename(ctx context.Context, id uuid.UUID, name string) (AccountView, error) {
	now := s.now()
	a, err := s.load(ctx, id)
	if err != nil {
		return AccountView{}, err
	}
	if err = a.CanWrite(now); err != nil {
		return AccountView{}, err
	}
	if err = a.SetDisplayName(name, now); err != nil {
		return AccountView{}, err
	}
	err = s.accounts.Save(ctx, &a, uuid.NewV7(), events.Event{
		ID: uuid.NewV7(), Kind: events.AccountRenamed, SubjectID: id, OccurredAt: now,
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return AccountView{}, apperr.ErrTryAgain
		}
		return AccountView{}, fmt.Errorf("save account: %w", err)
	}
	return viewOf(a, now), nil
}
