package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"
)

// stuckGrace is how long past its deadline a running segment may write
// its own end before the sweep closes it.
const stuckGrace = 30 * time.Second

// SweepTurns closes the turns running past their segment's deadline and
// stuckGrace as agent_stuck, with turn.finished, so the conversation takes
// questions again; a turn that wrote something can be continued.
func (s *Agent) SweepTurns(ctx context.Context) error {
	stuck, err := s.d.Runs.StuckTurns(ctx, s.d.Now(), stuckGrace, 100)
	if err != nil {
		return fmt.Errorf("find stuck turns: %w", err)
	}
	var errs []error
	for _, st := range stuck {
		if err = s.closeStuck(ctx, st.ID, st.RunStartedAt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// limiter allows each member perMinute questions and updates a minute.
type limiter struct {
	mu   sync.Mutex
	hits map[uuid.UUID][]time.Time
}

const perMinute = 30

func newLimiter() *limiter { return &limiter{hits: map[uuid.UUID][]time.Time{}} }

// allow counts one request of member at now, and says whether it is
// within the limit.
func (l *limiter) allow(member uuid.UUID, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.hits[member][:0]
	for _, t := range l.hits[member] {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	if len(kept) >= perMinute {
		l.hits[member] = kept
		return false
	}
	l.hits[member] = append(kept, now)
	return true
}
