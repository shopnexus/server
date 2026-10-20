package accounttest

import (
	"context"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
)

// MemStore is an in-memory account.Store for use case tests.
type MemStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]account.Account
}

func NewMemStore() *MemStore { return &MemStore{rows: map[uuid.UUID]account.Account{}} }

func micros(t time.Time) time.Time { return t.Truncate(time.Microsecond) }

// asStored returns a as Postgres keeps it: times at microsecond precision
// and write ID w.
func asStored(a account.Account, w uuid.UUID) account.Account {
	return account.Restore(a.ID(), a.DisplayName(), a.StoredStatus(), a.SuspendedUntil(), a.SuspendReason(), w,
		micros(a.CreatedAt()), micros(a.UpdatedAt()))
}

func (s *MemStore) Create(_ context.Context, a account.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[a.ID()]; !ok {
		s.rows[a.ID()] = asStored(a, a.WriteID())
	}
	return nil
}

func (s *MemStore) Get(_ context.Context, id uuid.UUID) (account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.rows[id]
	if !ok {
		return account.Account{}, account.ErrNotFound
	}
	return a, nil
}

func (s *MemStore) Save(_ context.Context, a *account.Account, writeID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rows[a.ID()]
	if !ok || (cur.WriteID() != a.WriteID() && cur.WriteID() != writeID) {
		return account.ErrConflict
	}
	*a = asStored(*a, writeID)
	s.rows[a.ID()] = *a
	return nil
}

func (s *MemStore) Delete(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	return nil
}
