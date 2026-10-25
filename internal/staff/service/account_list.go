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
// names by ID, phone number or email. A q that names nothing gives an empty
// page.
func (s *Staff) ListAccounts(ctx context.Context, q string, p page.Request) (page.Page[accountsvc.Record], error) {
	if q == "" {
		rows, err := s.accounts.Records(ctx, p)
		if err != nil {
			return page.Page[accountsvc.Record]{}, fmt.Errorf("list accounts: %w", err)
		}
		return page.Of(rows, p, func(r accountsvc.Record) uuid.UUID { return r.ID }), nil
	}
	out := page.Page[accountsvc.Record]{Items: []accountsvc.Record{}}
	id, parseErr := uuid.Parse(q)
	if parseErr != nil {
		found, ok, err := s.auth.FindAccount(ctx, q)
		if err != nil {
			return page.Page[accountsvc.Record]{}, fmt.Errorf("find account: %w", err)
		}
		if !ok {
			return out, nil
		}
		id = found
	}
	r, err := s.accounts.Record(ctx, id)
	if errors.Is(err, accountdomain.ErrAccountNotFound) {
		return out, nil
	}
	if err != nil {
		return page.Page[accountsvc.Record]{}, fmt.Errorf("get account: %w", err)
	}
	out.Items = append(out.Items, r)
	return out, nil
}
