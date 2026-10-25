package service

import (
	"context"
	"fmt"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/audit"
)

// SuspendAccount suspends a customer account; module account records
// account.suspend in the account's own transaction.
func (s *Staff) SuspendAccount(ctx context.Context, in accountsvc.SuspendInput) (accountsvc.Record, error) {
	r, err := s.accounts.Suspend(ctx, in)
	if err != nil {
		return accountsvc.Record{}, fmt.Errorf("suspend account: %w", err)
	}
	return r, nil
}

// UnsuspendAccount lifts a suspension and records account.unsuspend for by.
func (s *Staff) UnsuspendAccount(ctx context.Context, id uuid.UUID, by audit.Actor) (accountsvc.Record, error) {
	r, err := s.accounts.Reinstate(ctx, id, by)
	if err != nil {
		return accountsvc.Record{}, fmt.Errorf("unsuspend account: %w", err)
	}
	return r, nil
}
