package stream

import (
	"context"
	"log/slog"
	"sync"
	"uuid"
)

// buffer is how many hints wait for a slow stream; more are dropped, and
// the app catches up on the next hint or reconnect.
const buffer = 16

// Hub hands the hints it hears to the streams of their accounts.
type Hub struct {
	bell   Bell
	log    *slog.Logger
	mu     sync.Mutex
	subs   map[uuid.UUID]map[chan Hint]struct{}
	closed bool
}

func NewHub(bell Bell, log *slog.Logger) *Hub {
	return &Hub{bell: bell, log: log, subs: map[uuid.UUID]map[chan Hint]struct{}{}}
}

// Subscribe returns the hints for account until stop is called, or until
// Close, which closes the channel. After Close the channel comes closed.
func (h *Hub) Subscribe(account uuid.UUID) (hints <-chan Hint, stop func()) {
	ch := make(chan Hint, buffer)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(ch)
		return ch, func() {}
	}
	if h.subs[account] == nil {
		h.subs[account] = map[chan Hint]struct{}{}
	}
	h.subs[account][ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[account][ch]; !ok {
			return
		}
		delete(h.subs[account], ch)
		if len(h.subs[account]) == 0 {
			delete(h.subs, account)
		}
	}
}

// Close ends every stream: it closes the subscribers' channels, so the
// handlers return and a server shutdown does not wait for them. The app
// reconnects to another instance and reloads.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for _, chans := range h.subs {
		for ch := range chans {
			close(ch)
		}
	}
	h.subs = map[uuid.UUID]map[chan Hint]struct{}{}
}

// Run listens until ctx is done, then returns nil. Hints sent while the
// connection is lost are lost too, and the apps reload on their next hint.
func (h *Hub) Run(ctx context.Context) error {
	h.bell.Listen(ctx, h.deliver, func() {})
	return nil
}

// deliver hands the hint to every stream of its targets without waiting.
func (h *Hub) deliver(targets []Target, hint Hint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range targets {
		for ch := range h.subs[t.Account] {
			select {
			case ch <- hint:
			default:
			}
		}
	}
}
