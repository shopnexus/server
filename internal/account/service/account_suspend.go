package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/events"
)

// Suspend stops an account from writing, until in.Until or, with a nil
// Until, until Unsuspend. It records account.suspend for in.By in the same
// transaction.
func (s *Account) Suspend(ctx context.Context, in SuspendInput) (ConsoleAccount, error) {
	now := s.now()
	a, err := s.load(ctx, in.ID)
	if err != nil {
		return ConsoleAccount{}, err
	}
	if suspendErr := a.Suspend(in.Reason, in.Until, now); suspendErr != nil {
		return ConsoleAccount{}, suspendErr
	}
	r := consoleAccountOf(a, now)
	details := map[string]any{"reason": r.SuspendReason}
	ev := events.Event{ID: uuid.NewV7(), Kind: events.AccountSuspended, SubjectID: in.ID, OccurredAt: now}
	if r.SuspendedUntil != nil {
		details["until"] = *r.SuspendedUntil
		ev.Payload = map[string]any{"until": r.SuspendedUntil.Format(time.RFC3339)}
	}
	err = s.accounts.Save(ctx, &a, uuid.NewV7(), &audit.Entry{
		Actor: in.By, Action: audit.AccountSuspend, Target: audit.Account(in.ID), Details: details, At: now,
	}, ev)
	if errors.Is(err, repository.ErrConflict) {
		return ConsoleAccount{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ConsoleAccount{}, fmt.Errorf("save account: %w", err)
	}
	return r, nil
}

// Unsuspend lifts a suspension and records account.unsuspend for by.
func (s *Account) Unsuspend(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleAccount, error) {
	now := s.now()
	a, err := s.load(ctx, id)
	if err != nil {
		return ConsoleAccount{}, err
	}
	a.Unsuspend(now)
	err = s.accounts.Save(ctx, &a, uuid.NewV7(), &audit.Entry{
		Actor: by, Action: audit.AccountUnsuspend, Target: audit.Account(id), At: now,
	}, events.Event{ID: uuid.NewV7(), Kind: events.AccountUnsuspended, SubjectID: id, OccurredAt: now})
	if errors.Is(err, repository.ErrConflict) {
		return ConsoleAccount{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ConsoleAccount{}, fmt.Errorf("save account: %w", err)
	}
	return consoleAccountOf(a, now), nil
}
