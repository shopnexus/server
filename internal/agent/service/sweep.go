package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/repository"
)

// stuckGrace is how long past its deadline a run may write its own end
// before the sweep closes it.
const stuckGrace = time.Minute

// SweepTurns fails the turns running past their deadline and stuckGrace,
// whose replica lost the run's stream (it stopped, or the runtime did), so
// the conversation takes messages again; the turn can resume.
func (s *Agent) SweepTurns(ctx context.Context) error {
	stuck, err := s.d.Conversations.StuckTurns(ctx, s.d.Now(), stuckGrace, 100)
	if err != nil {
		return fmt.Errorf("find stuck turns: %w", err)
	}
	var errs []error
	for _, st := range stuck {
		end := repository.RunEnd{
			TurnID: st.TurnID, RunStartedAt: st.RunStartedAt, Status: domain.TurnFailed,
			Error: "the server lost the run's stream", At: s.d.Now().UTC(),
		}
		b, _ := json.Marshal(map[string]any{"status": end.Status, "error": end.Error, "usage": Usage{}})
		if _, err = s.d.Conversations.FinishRun(ctx, end, "turn.finished", b); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// limiter allows each member perMinute messages and steers a minute.
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
