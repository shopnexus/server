package accounttest

import (
	"context"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

// MemStore is an in-memory account.Store for use case tests. The contract
// suite runs against it and against the Postgres store, so the two agree.
type MemStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]account.Account
}

func NewMemStore() *MemStore { return &MemStore{rows: map[uuid.UUID]account.Account{}} }

func (s *MemStore) Insert(_ context.Context, _ postgres.Querier, a *account.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	created := a.CreatedAt().Truncate(time.Microsecond)
	*a = account.Restore(uuid.NewV7(), a.DisplayName(), a.Status(a.CreatedAt()), nil, "", created, created)
	s.rows[a.ID()] = *a
	return nil
}

func (s *MemStore) Get(_ context.Context, _ postgres.Querier, id uuid.UUID) (account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.rows[id]
	if !ok {
		return account.Account{}, account.ErrNotFound
	}
	return a, nil
}

func (s *MemStore) Update(_ context.Context, _ postgres.Querier, a account.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[a.ID()]; !ok {
		return account.ErrNotFound
	}
	s.rows[a.ID()] = a
	return nil
}
