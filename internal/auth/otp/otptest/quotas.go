package otptest

import (
	"context"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/otp"
)

// Quotas is an in-memory otp.QuotaStore.
type Quotas struct {
	mu   sync.Mutex
	rows map[string]otp.Quota
}

func NewQuotas() *Quotas { return &Quotas{rows: map[string]otp.Quota{}} }

func (s *Quotas) Get(_ context.Context, key string) (otp.Quota, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q, ok := s.rows[key]; ok {
		return q, nil
	}
	return otp.NewQuota(key), nil
}

func (s *Quotas) Save(_ context.Context, q *otp.Quota, writeID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rows[q.Key()]
	if ok && cur.WriteID() != q.WriteID() && cur.WriteID() != writeID {
		return otp.ErrConflict
	}
	if !ok && q.WriteID() != (uuid.UUID{}) {
		return otp.ErrConflict
	}
	sentAt := q.SentAt()
	for i, t := range sentAt {
		sentAt[i] = t.Truncate(time.Microsecond)
	}
	*q = otp.RestoreQuota(q.Key(), sentAt, writeID)
	s.rows[q.Key()] = *q
	return nil
}
