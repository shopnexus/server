package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
)

// Suspend stops an account from writing, until in.Until or, with a nil
// Until, until Reinstate. It records account.suspend for in.By in the same
// transaction.
func (s *Account) Suspend(ctx context.Context, in SuspendInput) (Record, error) {
	now := s.now()
	a, err := s.load(ctx, in.ID)
	if err != nil {
		return Record{}, err
	}
	if suspendErr := a.Suspend(in.Reason, in.Until, now); suspendErr != nil {
		return Record{}, suspendErr
	}
	r := recordOf(a, now)
	details := map[string]any{"reason": r.SuspendReason}
	if r.SuspendedUntil != nil {
		details["until"] = *r.SuspendedUntil
	}
	err = s.accounts.SaveAudited(ctx, &a, uuid.NewV7(), audit.Entry{
		Actor: in.By, Action: audit.AccountSuspend, Target: audit.Account(in.ID), Details: details, At: now,
	})
	if errors.Is(err, repository.ErrConflict) {
		return Record{}, apperr.ErrTryAgain
	}
	if err != nil {
		return Record{}, fmt.Errorf("save account: %w", err)
	}
	return r, nil
}

// Reinstate lifts a suspension and records account.unsuspend for by.
func (s *Account) Reinstate(ctx context.Context, id uuid.UUID, by audit.Actor) (Record, error) {
	now := s.now()
	a, err := s.load(ctx, id)
	if err != nil {
		return Record{}, err
	}
	a.Reinstate(now)
	err = s.accounts.SaveAudited(ctx, &a, uuid.NewV7(), audit.Entry{
		Actor: by, Action: audit.AccountUnsuspend, Target: audit.Account(id), At: now,
	})
	if errors.Is(err, repository.ErrConflict) {
		return Record{}, apperr.ErrTryAgain
	}
	if err != nil {
		return Record{}, fmt.Errorf("save account: %w", err)
	}
	return recordOf(a, now), nil
}
