package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/page"
)

// ListAccounts lists accounts newest first or, with in.Q, the one account Q
// names by ID or phone number. A Q that names nothing gives an empty page.
// With in.Suspended it lists only accounts suspended now, the suspension
// that ends soonest first, and counts them; with Q as well, the account Q
// names only if it is suspended.
func (s *Console) ListAccounts(ctx context.Context, in AccountQuery, p page.Request) (AccountList, error) {
	var out AccountList
	if in.Suspended {
		n, err := s.accounts.SuspendedCount(ctx)
		if err != nil {
			return AccountList{}, fmt.Errorf("count suspended accounts: %w", err)
		}
		out.Total = &n
	}
	switch {
	case in.Q == "" && in.Suspended:
		list, err := s.accounts.SuspendedAccounts(ctx, p)
		if err != nil {
			return AccountList{}, fmt.Errorf("list suspended accounts: %w", err)
		}
		out.Page = list
		return out, nil
	case in.Q == "":
		list, err := s.accounts.ConsoleAccounts(ctx, p)
		if err != nil {
			return AccountList{}, fmt.Errorf("list accounts: %w", err)
		}
		out.Page = list
		return out, nil
	}
	out.Items = []accountsvc.ConsoleAccount{}
	id, parseErr := uuid.Parse(in.Q)
	if parseErr != nil {
		found, ok, err := s.accounts.FindByPhone(ctx, in.Q)
		if err != nil {
			return AccountList{}, fmt.Errorf("find account: %w", err)
		}
		if !ok {
			return out, nil
		}
		id = found
	}
	r, err := s.accounts.ConsoleAccount(ctx, id)
	if errors.Is(err, accountdomain.ErrAccountNotFound) {
		return out, nil
	}
	if err != nil {
		return AccountList{}, fmt.Errorf("get account: %w", err)
	}
	if in.Suspended && r.Status != accountdomain.StatusSuspended {
		return out, nil
	}
	out.Items = append(out.Items, r)
	return out, nil
}
