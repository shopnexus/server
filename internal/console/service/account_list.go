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

// ListAccounts lists accounts newest first or, with q, the one account q
// names by ID or phone number. A q that names nothing gives an empty page.
func (s *Console) ListAccounts(ctx context.Context, q string, p page.Request) (page.Page[accountsvc.ConsoleAccount], error) {
	if q == "" {
		list, err := s.accounts.ConsoleAccounts(ctx, p)
		if err != nil {
			return page.Page[accountsvc.ConsoleAccount]{}, fmt.Errorf("list accounts: %w", err)
		}
		return list, nil
	}
	out := page.Page[accountsvc.ConsoleAccount]{Items: []accountsvc.ConsoleAccount{}}
	id, parseErr := uuid.Parse(q)
	if parseErr != nil {
		found, ok, err := s.accounts.FindByPhone(ctx, q)
		if err != nil {
			return page.Page[accountsvc.ConsoleAccount]{}, fmt.Errorf("find account: %w", err)
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
		return page.Page[accountsvc.ConsoleAccount]{}, fmt.Errorf("get account: %w", err)
	}
	out.Items = append(out.Items, r)
	return out, nil
}
