package restatetest

import (
	"sync"
	"time"
)

// Clock is a service's Now that a test moves by hand. Restate runs handlers
// on other goroutines, so it locks.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

func NewClock(start time.Time) *Clock { return &Clock{mu: sync.Mutex{}, t: start} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *Clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
