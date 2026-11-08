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
	r, err := s.accounts.ConsoleAccount(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("get account: %w", err)
	}
	ids, err := s.accounts.Identities(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list identities: %w", err)
	}
	acts, err := s.actions.List(ctx, audit.Filter{TargetID: id}, page.Request{Limit: historyLength})
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list console actions: %w", err)
	}
	// The list read one more than it needs; the copy is never nil, so the
	// JSON is an array.
	latest := append([]audit.Action{}, acts[:min(len(acts), historyLength)]...)
	return AccountDetail{Account: r, Identities: ids, Actions: latest}, nil
}
