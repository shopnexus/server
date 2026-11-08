package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// historyLength is how many console actions the account screen shows.
const historyLength = 20

// GetAccount reads an account with its identities and the latest console
// actions on it.
func (s *Console) GetAccount(ctx context.Context, id uuid.UUID) (AccountDetail, error) {
	r, err := s.accounts.Record(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("get account: %w", err)
	}
	ids, err := s.accounts.Identities(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list identities: %w", err)
	}
	history := page.Request{Limit: historyLength}
	acts, err := s.actions.List(ctx, audit.Filter{TargetID: id}, history)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list console actions: %w", err)
	}
	latest := page.Of(acts, history, func(a audit.Action) uuid.UUID { return a.ID })
	return AccountDetail{Account: r, Identities: ids, Actions: latest.Items}, nil
}
