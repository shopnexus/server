package stream

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"uuid"
)

const (
	// queue is how many events wait for a slow connection; past it the
	// connection reads the log instead.
	queue      = 16
	pruneEvery = time.Hour
	// kindProbe is the hub's own ring; it reaches no connection.
	kindProbe = "probe"
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
	// probeEvery is how often the hub rings itself to check that it still
	// hears the bell, and touches the accounts connected to it.
	probeEvery time.Duration
	// active is set by KeepActive before Run.
	active Active
	heard  atomic.Bool
	mu     sync.Mutex
	subs   map[uuid.UUID]map[*Sub]struct{}
	closed bool
}

func NewHub(bell Bell, updates UpdateLog, probeEvery time.Duration, now func() time.Time, log *slog.Logger) *Hub {
	return &Hub{
		bell: bell, updates: updates, probeEvery: probeEvery, now: now, log: log,
		done: make(chan struct{}), subs: map[uuid.UUID]map[*Sub]struct{}{},
	}
}

// KeepActive has Run touch the accounts connected to the hub every
// probeEvery, in one write for them all. It is called before Run.
func (h *Hub) KeepActive(a Active) { h.active = a }

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
	if err := h.bell.Ring(ctx, []Target{{Account: to}}, Hint{Kind: KindTyping, ConversationID: &conversation}); err != nil {
		return fmt.Errorf("ring typing: %w", err)
	}
	return nil
}

// Run listens for rings, prunes the log at start and every hour, and
// probes the bell and touches the connected accounts every probeEvery,
// until ctx is done; then it returns nil.
func (h *Hub) Run(ctx context.Context) error {
	var listening sync.WaitGroup
	listening.Go(func() { h.bell.Listen(ctx, h.deliver, h.resync) })
	defer listening.Wait()
	prune := time.NewTicker(pruneEvery)
	defer prune.Stop()
	probe := time.NewTicker(h.probeEvery)
	defer probe.Stop()
	h.heard.Store(true)
	h.prune(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-prune.C:
			h.prune(ctx)
		case <-probe.C:
			// A probe not heard back means a LISTEN connection that died in
			// silence: every connection reads the log, so a lost ring costs
			// at most one probe.
			if !h.heard.Swap(false) {
				h.resync()
			}
			if err := h.bell.Ring(ctx, nil, Hint{Kind: kindProbe}); err != nil && ctx.Err() == nil {
				h.log.WarnContext(ctx, "stream: probe the bell", "err", err)
			}
			h.touch(ctx)
		}
	}
}

func (h *Hub) prune(ctx context.Context) {
	if _, err := h.updates.Prune(ctx, h.now().Add(-keepUpdates)); err != nil && ctx.Err() == nil {
		h.log.WarnContext(ctx, "stream: prune the update log", "err", err)
	}
}

// touch records that the accounts connected to the hub are active.
func (h *Hub) touch(ctx context.Context) {
	if h.active == nil {
		return
	}
	h.mu.Lock()
	accounts := slices.Collect(maps.Keys(h.subs))
	h.mu.Unlock()
	if len(accounts) == 0 {
		return
	}
	if err := h.active.Touch(ctx, accounts); err != nil && ctx.Err() == nil {
		h.log.WarnContext(ctx, "stream: touch the connected accounts", "err", err)
	}
}

// deliver queues the event for every subscription of its targets without
// waiting. An update that does not fit marks the subscription behind; a
// typing event that does not fit is dropped.
func (h *Hub) deliver(targets []Target, hint Hint) {
	if hint.Kind == kindProbe {
		h.heard.Store(true)
		return
	}
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
