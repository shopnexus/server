package service

import (
	"context"
	"fmt"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/audit"
)

// SuspendAccount suspends a customer account; module account records
// account.suspend in the account's own transaction.
func (s *Console) SuspendAccount(ctx context.Context, in accountsvc.SuspendInput) (accountsvc.ConsoleAccount, error) {
	r, err := s.accounts.Suspend(ctx, in)
	if err != nil {
		return accountsvc.ConsoleAccount{}, fmt.Errorf("suspend account: %w", err)
	}
	return r, nil
}

// UnsuspendAccount lifts a suspension and records account.unsuspend for by.
func (s *Console) UnsuspendAccount(ctx context.Context, id uuid.UUID, by audit.Actor) (accountsvc.ConsoleAccount, error) {
	r, err := s.accounts.Unsuspend(ctx, id, by)
	if err != nil {
		return accountsvc.ConsoleAccount{}, fmt.Errorf("unsuspend account: %w", err)
	}
	return r, nil
}
