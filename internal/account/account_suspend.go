package account

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/audit"
)

// Suspend stops an account from writing, until the given time or, with a nil
// until, until Reinstate. It records account.suspend for by in the same
// transaction.
func Suspend(ctx context.Context, accounts Store, id uuid.UUID, reason string, until *time.Time, by audit.Actor, now time.Time) (Record, error) {
	a, err := accounts.Get(ctx, id)
	if err != nil {
		return Record{}, fmt.Errorf("get account: %w", err)
	}
	if suspendErr := a.Suspend(reason, until, now); suspendErr != nil {
		return Record{}, suspendErr
	}
	r := recordOf(a, now)
	details := map[string]any{"reason": r.SuspendReason}
	if r.SuspendedUntil != nil {
		details["until"] = *r.SuspendedUntil
	}
	err = accounts.SaveAudited(ctx, &a, uuid.NewV7(), audit.Entry{
		Actor: by, Action: "account.suspend", Target: audit.Account(id), Details: details, At: now,
	})
	if errors.Is(err, ErrConflict) {
		return Record{}, apperr.ErrTryAgain
	}
	if err != nil {
		return Record{}, fmt.Errorf("save account: %w", err)
	}
	return r, nil
}

// Reinstate lifts a suspension and records account.unsuspend for by.
func Reinstate(ctx context.Context, accounts Store, id uuid.UUID, by audit.Actor, now time.Time) (Record, error) {
	a, err := accounts.Get(ctx, id)
	if err != nil {
		return Record{}, fmt.Errorf("get account: %w", err)
	}
	a.Reinstate(now)
	err = accounts.SaveAudited(ctx, &a, uuid.NewV7(), audit.Entry{
		Actor: by, Action: "account.unsuspend", Target: audit.Account(id), At: now,
	})
	if errors.Is(err, ErrConflict) {
		return Record{}, apperr.ErrTryAgain
	}
	if err != nil {
		return Record{}, fmt.Errorf("save account: %w", err)
	}
	return recordOf(a, now), nil
}
