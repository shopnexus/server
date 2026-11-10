package stream

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"uuid"
)

const (
	// queue is how many events wait for a slow connection; past it the
	// connection reads the log instead.
	queue      = 16
	pruneEvery = time.Hour
)

// Event is what the hub hands a connection: an update at Seq, or typing
// with Seq 0.
type Event struct {
	Seq  int64
	Hint Hint
}

// Sub is one connection's subscription to its account's events.
type Sub struct {
	events chan Event
	behind chan struct{}
}

// Events delivers the account's events in the order they were rung.
func (s *Sub) Events() <-chan Event { return s.events }

// Behind signals that an update may have been missed, because the queue was
// full or the bell was not listening; the connection reads the log from its
// last seq.
func (s *Sub) Behind() <-chan struct{} { return s.behind }

func (s *Sub) fallBehind() {
	select {
	case s.behind <- struct{}{}:
	default:
	}
}

// Hub hands the rings it hears to the subscriptions of their accounts, and
// prunes the update log.
type Hub struct {
	bell    Bell
	updates UpdateLog
	now     func() time.Time
	log     *slog.Logger
	done    chan struct{}
	mu      sync.Mutex
	subs    map[uuid.UUID]map[*Sub]struct{}
	closed  bool
}

func NewHub(bell Bell, updates UpdateLog, now func() time.Time, log *slog.Logger) *Hub {
	return &Hub{
		bell: bell, updates: updates, now: now, log: log,
		done: make(chan struct{}), subs: map[uuid.UUID]map[*Sub]struct{}{},
	}
}

// Subscribe returns a subscription to account's events until stop is
// called.
func (h *Hub) Subscribe(account uuid.UUID) (sub *Sub, stop func()) {
	s := &Sub{events: make(chan Event, queue), behind: make(chan struct{}, 1)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[account] == nil {
		h.subs[account] = map[*Sub]struct{}{}
	}
	h.subs[account][s] = struct{}{}
	return s, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[account], s)
		if len(h.subs[account]) == 0 {
			delete(h.subs, account)
		}
	}
}

// Done is closed by Close.
func (h *Hub) Done() <-chan struct{} { return h.done }

// Close tells every connection to end with 1001, so a server shutdown does
// not wait for them; the apps reconnect to another replica with their seq.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.done)
}

// Typing rings to, at once, that the other member types in conversation.
func (h *Hub) Typing(ctx context.Context, to, conversation uuid.UUID) error {
	if err := h.bell.Ring(ctx, nil, []Target{{Account: to}}, Hint{Kind: KindTyping, ConversationID: &conversation}); err != nil {
		return fmt.Errorf("ring typing: %w", err)
	}
	return nil
}

// Run listens for rings, and prunes the log at start and every hour, until
// ctx is done; then it returns nil.
func (h *Hub) Run(ctx context.Context) error {
	var listening sync.WaitGroup
	listening.Go(func() { h.bell.Listen(ctx, h.deliver, h.resync) })
	defer listening.Wait()
	tick := time.NewTicker(pruneEvery)
	defer tick.Stop()
	for {
		if _, err := h.updates.Prune(ctx, h.now().Add(-keepUpdates)); err != nil && ctx.Err() == nil {
			h.log.WarnContext(ctx, "stream: prune the update log", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// deliver queues the event for every subscription of its targets without
// waiting. An update that does not fit marks the subscription behind; a
// typing event that does not fit is dropped.
func (h *Hub) deliver(targets []Target, hint Hint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range targets {
		for s := range h.subs[t.Account] {
			select {
			case s.events <- Event{Seq: t.Seq, Hint: hint}:
			default:
				if t.Seq > 0 {
					s.fallBehind()
				}
			}
		}
	}
}

// resync marks every subscription behind: the bell was not listening, and
// rings may have been lost.
func (h *Hub) resync() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.subs {
		for s := range subs {
			s.fallBehind()
		}
	}
}
