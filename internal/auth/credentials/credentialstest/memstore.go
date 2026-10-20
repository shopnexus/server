package credentialstest

import (
	"context"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/credentials"
)

// MemStore is an in-memory credentials.Store.
type MemStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]credentials.Credentials
}

func NewMemStore() *MemStore { return &MemStore{rows: map[uuid.UUID]credentials.Credentials{}} }

// asStored returns c as Postgres keeps it: times at microsecond precision
// and write ID w.
func asStored(c credentials.Credentials, w uuid.UUID) credentials.Credentials {
	var ids []credentials.Identity
	for _, i := range c.Identities() {
		used := i.LastUsedAt()
		if used != nil {
			v := used.Truncate(time.Microsecond)
			used = &v
		}
		ids = append(ids, credentials.RestoreIdentity(i.ID(), i.Kind(), i.Value(), used, i.CreatedAt().Truncate(time.Microsecond)))
	}
	return credentials.Restore(c.AccountID(), ids, w, c.UpdatedAt().Truncate(time.Microsecond))
}

func (s *MemStore) takenByOther(c credentials.Credentials) bool {
	for acc, other := range s.rows {
		if acc == c.AccountID() {
			continue
		}
		for _, oi := range other.Identities() {
			for _, i := range c.Identities() {
				if oi.Kind() == i.Kind() && oi.Value() == i.Value() {
					return true
				}
			}
		}
	}
	return false
}

func (s *MemStore) Create(_ context.Context, c credentials.Credentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.takenByOther(c) {
		return credentials.ErrTaken
	}
	if _, ok := s.rows[c.AccountID()]; !ok {
		s.rows[c.AccountID()] = asStored(c, c.WriteID())
	}
	return nil
}

func (s *MemStore) Get(_ context.Context, accountID uuid.UUID) (credentials.Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[accountID]
	if !ok {
		return credentials.Credentials{}, credentials.ErrNotFound
	}
	return c, nil
}

func (s *MemStore) FindIdentity(_ context.Context, kind credentials.Kind, value string) (credentials.IdentityRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for acc, c := range s.rows {
		for _, i := range c.Identities() {
			if i.Kind() == kind && i.Value() == value {
				return credentials.IdentityRef{AccountID: acc, IdentityID: i.ID()}, nil
			}
		}
	}
	return credentials.IdentityRef{}, credentials.ErrNotFound
}

func (s *MemStore) Save(_ context.Context, c *credentials.Credentials, writeID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rows[c.AccountID()]
	if !ok || (cur.WriteID() != c.WriteID() && cur.WriteID() != writeID) {
		return credentials.ErrConflict
	}
	if s.takenByOther(*c) {
		return credentials.ErrTaken
	}
	*c = asStored(*c, writeID)
	s.rows[c.AccountID()] = *c
	return nil
}
