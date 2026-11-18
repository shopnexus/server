package domain_test

import (
	"testing"
	"time"

	"github.com/shopnexus/server/internal/chat/domain"
)

func TestReplyRateOf(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	answered := func(age, after time.Duration) domain.ReplyThread {
		at := now.Add(-age).Add(after)
		return domain.ReplyThread{Started: now.Add(-age), Answered: &at}
	}
	unanswered := func(age time.Duration) domain.ReplyThread { return domain.ReplyThread{Started: now.Add(-age)} }
	day := 24 * time.Hour
	inTime := []domain.ReplyThread{
		answered(2*day, time.Hour), answered(3*day, day), answered(10*day, time.Minute), answered(89*day, 2*time.Hour),
	}
	for name, c := range map[string]struct {
		threads        []domain.ReplyThread
		percent, count int
		ok             bool
	}{
		"four count, none":                 {inTime, 0, 4, false},
		"five in time":                     {append(inTime[:4:4], answered(day, time.Hour)), 100, 5, true},
		"an answer after 24 h is late":     {append(inTime[:4:4], answered(5*day, day+time.Minute)), 80, 5, true},
		"unanswered and old is late":       {append(inTime[:4:4], unanswered(day+time.Minute)), 80, 5, true},
		"unanswered and young is left out": {append(inTime[:4:4], unanswered(23*time.Hour)), 0, 4, false},
		"older than 90 days is left out":   {append(inTime[:4:4], answered(91*day, time.Hour)), 0, 4, false},
		"rounded down":                     {append(inTime[:4:4], unanswered(2*day), unanswered(2*day), answered(4*day, time.Hour)), 71, 7, true},
	} {
		percent, count, ok := domain.ReplyRateOf(c.threads, now)
		if percent != c.percent || count != c.count || ok != c.ok {
			t.Errorf("%s: %d%% of %d, %v; want %d%% of %d, %v", name, percent, count, ok, c.percent, c.count, c.ok)
		}
	}
}
