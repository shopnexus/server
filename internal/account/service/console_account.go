package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// ConsoleAccount reads one account as console members see it.
func (s *Account) ConsoleAccount(ctx context.Context, id uuid.UUID) (ConsoleAccount, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return ConsoleAccount{}, err
	}
	return consoleAccountOf(a, s.now()), nil
}

// ConsoleAccountsOf reads the accounts among ids in one query.
func (s *Account) ConsoleAccountsOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]ConsoleAccount, error) {
	rows, err := s.accounts.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}
	now := s.now()
	out := make(map[uuid.UUID]ConsoleAccount, len(rows))
	for i := range rows {
		out[rows[i].ID()] = consoleAccountOf(rows[i], now)
	}
	return out, nil
}

// ConsoleAccounts lists accounts as console members see them, newest first.
func (s *Account) ConsoleAccounts(ctx context.Context, p page.Request) (page.Page[ConsoleAccount], error) {
	list, err := s.accounts.List(ctx, p)
	if err != nil {
		return page.Page[ConsoleAccount]{}, fmt.Errorf("list accounts: %w", err)
	}
	now := s.now()
	out := make([]ConsoleAccount, 0, len(list))
	for i := range list {
		out = append(out, consoleAccountOf(list[i], now))
	}
	return page.Of(out, p, func(a ConsoleAccount) uuid.UUID { return a.ID }), nil
}

// SuspendedAccounts lists the accounts whose suspension is in force now: the
// suspension that ends soonest first, those without an end last.
func (s *Account) SuspendedAccounts(ctx context.Context, p page.Request) (page.Page[ConsoleAccount], error) {
	now := s.now()
	list, err := s.accounts.ListSuspended(ctx, now, p)
	if err != nil {
		return page.Page[ConsoleAccount]{}, fmt.Errorf("list suspended accounts: %w", err)
	}
	out := make([]ConsoleAccount, 0, len(list))
	for i := range list {
		out = append(out, consoleAccountOf(list[i], now))
	}
	return page.Of(out, p, func(a ConsoleAccount) uuid.UUID { return a.ID }), nil
}

// SuspendedCount is how many accounts SuspendedAccounts lists in all.
func (s *Account) SuspendedCount(ctx context.Context) (int, error) {
	n, err := s.accounts.CountSuspended(ctx, s.now())
	if err != nil {
		return 0, fmt.Errorf("count suspended accounts: %w", err)
	}
	return n, nil
}

// CountByStatus counts the accounts created at or after since in each
// status now.
func (s *Account) CountByStatus(ctx context.Context, since time.Time) (map[domain.Status]int, error) {
	n, err := s.accounts.CountByStatus(ctx, s.now(), since)
	if err != nil {
		return nil, fmt.Errorf("count accounts: %w", err)
	}
	return n, nil
}
