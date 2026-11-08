package service

import (
	"context"
	"fmt"
	"uuid"

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
