package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
)

// Get reads an account.
func (s *Account) Get(ctx context.Context, id uuid.UUID) (AccountView, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return AccountView{}, err
	}
	return viewOf(a, s.now()), nil
}

// Status returns the status in force now.
func (s *Account) Status(ctx context.Context, id uuid.UUID) (domain.Status, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return "", err
	}
	return a.Status(s.now()), nil
}

// load reads an account; an unknown id is domain.ErrAccountNotFound, which
// a client of Service sees whatever stores the account.
func (s *Account) load(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	a, err := s.accounts.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("get account: %w", err)
	}
	return a, nil
}
