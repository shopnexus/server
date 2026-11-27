package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
)

// Get reads an account.
func (s *Account) Get(ctx context.Context, id uuid.UUID) (AccountView, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return AccountView{}, err
	}
	return s.view(a, s.d.Now()), nil
}

// GetMany reads the accounts among ids in one query.
func (s *Account) GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]AccountView, error) {
	rows, err := s.d.Accounts.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}
	now := s.d.Now()
	out := make(map[uuid.UUID]AccountView, len(rows))
	for i := range rows {
		out[rows[i].ID()] = s.view(rows[i], now)
	}
	return out, nil
}

// Me reads the caller's account and its sign-in methods. A deleted account
// has none, and its access token answers session_revoked. A suspended one
// also reads why and until when (BR-MOD-02).
//
// Me is UC-ACC-06, step 1.
func (s *Account) Me(ctx context.Context, id uuid.UUID) (MeView, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return MeView{}, err
	}
	if a.StoredStatus() == domain.StatusDeleted {
		return MeView{}, domain.ErrSessionRevoked
	}
	ids, err := s.Identities(ctx, id)
	if err != nil {
		return MeView{}, err
	}
	now := s.d.Now()
	v := MeView{AccountView: s.view(a, now), Identities: ids}
	if a.Suspended(now) {
		v.Suspension = &SuspensionView{Reason: a.SuspendReason(), Until: a.SuspendedUntil()}
	}
	return v, nil
}

func (s *Account) Status(ctx context.Context, id uuid.UUID) (domain.Status, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return "", err
	}
	return a.Status(s.d.Now()), nil
}

func (s *Account) Away(ctx context.Context, id uuid.UUID) (bool, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return false, err
	}
	return a.Away(s.d.Now()), nil
}

// load reads an account; an unknown id is domain.ErrAccountNotFound, which
// a client of Service sees whatever stores the account.
func (s *Account) load(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	a, err := s.d.Accounts.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("get account: %w", err)
	}
	return a, nil
}
