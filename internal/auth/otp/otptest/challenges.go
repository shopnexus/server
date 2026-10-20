package otptest

import (
	"context"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/otp"
)

// Challenges is an in-memory otp.ChallengeStore.
type Challenges struct {
	mu   sync.Mutex
	rows map[uuid.UUID]otp.Challenge
}

func NewChallenges() *Challenges { return &Challenges{rows: map[uuid.UUID]otp.Challenge{}} }

func micros(t time.Time) time.Time { return t.Truncate(time.Microsecond) }

// asStored returns c as Postgres keeps it: times at microsecond precision
// and write ID w.
func asStored(c otp.Challenge, w uuid.UUID) otp.Challenge {
	consumed := c.ConsumedAt()
	if consumed != nil {
		v := micros(*consumed)
		consumed = &v
	}
	return otp.Restore(c.ID(), c.Purpose(), c.Destination(), c.AccountID(), c.CodeHash(), c.Attempts(), c.IP(),
		micros(c.ExpiresAt()), micros(c.CreatedAt()), consumed, w)
}

func (s *Challenges) Create(_ context.Context, c otp.Challenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[c.ID()]; !ok {
		s.rows[c.ID()] = asStored(c, c.WriteID())
	}
	return nil
}

func (s *Challenges) Get(_ context.Context, id uuid.UUID) (otp.Challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[id]
	if !ok {
		return otp.Challenge{}, otp.ErrNotFound
	}
	return c, nil
}

func (s *Challenges) Save(_ context.Context, c *otp.Challenge, writeID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.rows[c.ID()]
	if !ok || (cur.WriteID() != c.WriteID() && cur.WriteID() != writeID) {
		return otp.ErrConflict
	}
	*c = asStored(*c, writeID)
	s.rows[c.ID()] = *c
	return nil
}

func (s *Challenges) IsNewest(_ context.Context, c otp.Challenge) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.Destination().Value() != c.Destination().Value() || r.Purpose() != c.Purpose() {
			continue
		}
		if r.CreatedAt().After(c.CreatedAt()) || (r.CreatedAt().Equal(c.CreatedAt()) && r.ID().Compare(c.ID()) > 0) {
			return false, nil
		}
	}
	return true, nil
}
