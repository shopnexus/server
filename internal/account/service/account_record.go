package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

// Record reads one account as staff see it.
func (s *Account) Record(ctx context.Context, id uuid.UUID) (Record, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return Record{}, err
	}
	return recordOf(a, s.now()), nil
}

// Records lists accounts newest first. It returns the p.Fetch() rows the
// store read; the caller cuts the page.
func (s *Account) Records(ctx context.Context, p page.Request) ([]Record, error) {
	list, err := s.accounts.List(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	now := s.now()
	out := make([]Record, 0, len(list))
	for i := range list {
		out = append(out, recordOf(list[i], now))
	}
	return out, nil
}
