package sessiontest

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/session"
)

// MemStore is an in-memory session.Store.
type MemStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]session.Session
}

func NewMemStore() *MemStore { return &MemStore{rows: map[uuid.UUID]session.Session{}} }

func micros(t time.Time) time.Time { return t.Truncate(time.Microsecond) }

// asStored returns s as Postgres keeps it: times at microsecond precision
// and write ID w.
func asStored(s session.Session, w uuid.UUID) session.Session {
	revoked := s.RevokedAt()
	if revoked != nil {
		v := micros(*revoked)
		revoked = &v
	}
	return session.Restore(s.ID(), s.AccountID(), s.RefreshHash(), s.PrevRefreshHash(), s.DeviceName(), micros(s.AuthTime()),
		micros(s.CreatedAt()), micros(s.LastUsedAt()), micros(s.ExpiresAt()), revoked, w)
}

func (m *MemStore) Create(_ context.Context, s session.Session) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[s.ID()]; ok {
		return false, nil
	}
	m.rows[s.ID()] = asStored(s, s.WriteID())
	return true, nil
}

func (m *MemStore) find(match func(session.Session) bool) (session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.rows {
		if match(s) {
			return s, nil
		}
	}
	return session.Session{}, session.ErrNotFound
}

func (m *MemStore) Get(_ context.Context, id uuid.UUID) (session.Session, error) {
	return m.find(func(s session.Session) bool { return s.ID() == id })
}

func (m *MemStore) FindByRefresh(_ context.Context, hash []byte) (session.Session, error) {
	return m.find(func(s session.Session) bool { return bytes.Equal(s.RefreshHash(), hash) })
}

func (m *MemStore) FindByPrevRefresh(_ context.Context, hash []byte) (session.Session, error) {
	return m.find(func(s session.Session) bool {
		return s.PrevRefreshHash() != nil && bytes.Equal(s.PrevRefreshHash(), hash)
	})
}

func (m *MemStore) Save(_ context.Context, s *session.Session, writeID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.rows[s.ID()]
	if !ok || (cur.WriteID() != s.WriteID() && cur.WriteID() != writeID) {
		return session.ErrConflict
	}
	*s = asStored(*s, writeID)
	m.rows[s.ID()] = *s
	return nil
}

func (m *MemStore) RevokeAll(_ context.Context, accountID uuid.UUID, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.rows {
		if s.AccountID() == accountID && s.RevokedAt() == nil {
			s.Revoke(now)
			m.rows[id] = asStored(s, uuid.NewV7())
		}
	}
	return nil
}

func (m *MemStore) ListActive(_ context.Context, accountID uuid.UUID, now time.Time, before uuid.UUID, limit int) ([]session.View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []session.View
	for _, s := range m.rows {
		if s.AccountID() == accountID && s.Usable(now) == nil && (before == (uuid.UUID{}) || s.ID().Compare(before) < 0) {
			out = append(out, session.View{ID: s.ID(), DeviceName: s.DeviceName(), CreatedAt: s.CreatedAt(), LastUsedAt: s.LastUsedAt()})
		}
	}
	slices.SortFunc(out, func(a, b session.View) int { return b.ID.Compare(a.ID) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
