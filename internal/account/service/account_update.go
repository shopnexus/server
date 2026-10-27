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

// Update changes what in names. A new avatar is attached to the account
// and processed into a square by media before the save, so the reply
// carries its URL; a retry with the same upload gets the same square. It is
// a write, so a suspended account gets ErrSuspended even while its access
// token is still valid.
func (s *Account) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (AccountView, error) {
	now := s.now()
	a, err := s.load(ctx, id)
	if err != nil {
		return AccountView{}, err
	}
	if err = a.CanWrite(now); err != nil {
		return AccountView{}, err
	}
	if err = a.Edit(in.DisplayName, in.Bio, now); err != nil {
		return AccountView{}, err
	}
	switch {
	case in.RemoveAvatar:
		a.SetAvatar("", now)
	case in.Avatar != nil:
		key, squareErr := s.media.Square(ctx, id, id, *in.Avatar)
		if squareErr != nil {
			return AccountView{}, fmt.Errorf("make the avatar: %w", squareErr)
		}
		a.SetAvatar(key, now)
	default: // the avatar stays as it is
	}
	err = s.accounts.Save(ctx, &a, uuid.NewV7(), events.Event{
		ID: uuid.NewV7(), Kind: events.AccountUpdated, SubjectID: id, OccurredAt: now,
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return AccountView{}, apperr.ErrTryAgain
		}
		return AccountView{}, fmt.Errorf("save account: %w", err)
	}
	return s.view(a, now), nil
}
