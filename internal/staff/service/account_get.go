package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// historyLength is how many staff actions the account screen shows.
const historyLength = 20

// GetAccount reads an account with its identities and the latest staff
// actions on it.
func (s *Staff) GetAccount(ctx context.Context, id uuid.UUID) (AccountDetail, error) {
	r, err := s.accounts.Record(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("get account: %w", err)
	}
	ids, err := s.auth.Identities(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list identities: %w", err)
	}
	history := page.Request{Limit: historyLength}
	acts, err := s.actions.List(ctx, audit.Filter{TargetID: id}, history)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list staff actions: %w", err)
	}
	latest := page.Of(acts, history, func(a audit.Action) uuid.UUID { return a.ID })
	return AccountDetail{Account: r, Identities: ids, Actions: latest.Items}, nil
}
