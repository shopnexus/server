package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// ConsoleAccount reads one account as console members see it.
func (s *Account) ConsoleAccount(ctx context.Context, id uuid.UUID) (ConsoleAccount, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return ConsoleAccount{}, err
	}
	out := []ConsoleAccount{consoleAccountOf(a, s.d.Now())}
	if err = s.addContacts(ctx, out); err != nil {
		return ConsoleAccount{}, err
	}
	return out[0], nil
}

// Contacts returns each account's phone number, or its email address
// without one, for the accounts of ids that have either.
func (s *Account) Contacts(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out, err := s.d.Credentials.Contacts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get contacts: %w", err)
	}
	return out, nil
}

// addContacts fills the Contact of each of list in one query.
func (s *Account) addContacts(ctx context.Context, list []ConsoleAccount) error {
	ids := make([]uuid.UUID, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	contacts, err := s.Contacts(ctx, ids)
	if err != nil {
		return err
	}
	for i := range list {
		list[i].Contact = contacts[list[i].ID]
	}
	return nil
}

// ConsoleAccountsOf reads the accounts among ids in one query.
func (s *Account) ConsoleAccountsOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]ConsoleAccount, error) {
	rows, err := s.d.Accounts.GetMany(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}
	now := s.d.Now()
	list := make([]ConsoleAccount, len(rows))
	for i := range rows {
		list[i] = consoleAccountOf(rows[i], now)
	}
	if err = s.addContacts(ctx, list); err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]ConsoleAccount, len(list))
	for _, a := range list {
		out[a.ID] = a
	}
	return out, nil
}

// ConsoleAccounts lists accounts as console members see them, newest first.
func (s *Account) ConsoleAccounts(ctx context.Context, p page.Request) (page.Page[ConsoleAccount], error) {
	list, err := s.d.Accounts.List(ctx, p)
	if err != nil {
		return page.Page[ConsoleAccount]{}, fmt.Errorf("list accounts: %w", err)
	}
	now := s.d.Now()
	out := make([]ConsoleAccount, 0, len(list))
	for i := range list {
		out = append(out, consoleAccountOf(list[i], now))
	}
	if err = s.addContacts(ctx, out); err != nil {
		return page.Page[ConsoleAccount]{}, err
	}
	return page.Of(out, p, func(a ConsoleAccount) uuid.UUID { return a.ID }), nil
}

// SuspendedAccounts lists the accounts whose suspension is in force now: the
// suspension that ends soonest first, those without an end last.
func (s *Account) SuspendedAccounts(ctx context.Context, p page.Request) (page.Page[ConsoleAccount], error) {
	now := s.d.Now()
	list, err := s.d.Accounts.ListSuspended(ctx, now, p)
	if err != nil {
		return page.Page[ConsoleAccount]{}, fmt.Errorf("list suspended accounts: %w", err)
	}
	out := make([]ConsoleAccount, 0, len(list))
	for i := range list {
		out = append(out, consoleAccountOf(list[i], now))
	}
	if err = s.addContacts(ctx, out); err != nil {
		return page.Page[ConsoleAccount]{}, err
	}
	return page.Of(out, p, func(a ConsoleAccount) uuid.UUID { return a.ID }), nil
}

// SuspendedCount is how many accounts SuspendedAccounts lists in all.
func (s *Account) SuspendedCount(ctx context.Context) (int, error) {
	n, err := s.d.Accounts.CountSuspended(ctx, s.d.Now())
	if err != nil {
		return 0, fmt.Errorf("count suspended accounts: %w", err)
	}
	return n, nil
}

// CountByStatus counts the accounts created at or after since in each
// status now.
func (s *Account) CountByStatus(ctx context.Context, since time.Time) (map[domain.Status]int, error) {
	n, err := s.d.Accounts.CountByStatus(ctx, s.d.Now(), since)
	if err != nil {
		return nil, fmt.Errorf("count accounts: %w", err)
	}
	return n, nil
}
