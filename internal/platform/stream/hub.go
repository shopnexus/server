package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// buffer is how many hints wait for a slow stream; more are dropped,
	// and the app catches up on the next hint or reconnect.
	buffer = 16
	// retryEvery is the pause before listening again on a lost connection.
	retryEvery = time.Second
)

// Hub hands the hints it hears to the streams of their accounts.
type Hub struct {
	db     *pgxpool.Pool
	log    *slog.Logger
	mu     sync.Mutex
	subs   map[uuid.UUID]map[chan Hint]struct{}
	closed bool
}

func NewHub(db *pgxpool.Pool, log *slog.Logger) *Hub {
	return &Hub{db: db, log: log, subs: map[uuid.UUID]map[chan Hint]struct{}{}}
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

// Run listens until ctx is done, then returns nil. A lost connection is
// listened again after a pause; hints sent meanwhile are lost, and the apps
// reload on their next hint.
func (h *Hub) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := h.listen(ctx); err != nil && ctx.Err() == nil {
			h.log.WarnContext(ctx, "stream: listen", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(retryEvery):
			}
		}
	}
	return nil
}

// listen holds a connection of its own, outside the pool, as events.Bus
// does: a pooled one would hold a slot for good.
func (h *Hub) listen(ctx context.Context) error {
	conn, err := pgx.ConnectConfig(ctx, h.db.Config().ConnConfig.Copy())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err = conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	for {
		n, waitErr := conn.WaitForNotification(ctx)
		if waitErr != nil {
			return fmt.Errorf("wait for notification: %w", waitErr)
		}
		var msg notification
		if err = json.Unmarshal([]byte(n.Payload), &msg); err != nil {
			h.log.WarnContext(ctx, "stream: bad notification", "err", err)
			continue
		}
		msg.Hint.Kind = msg.Kind
		h.deliver(msg)
	}
}

// deliver hands the hint to every stream of its accounts without waiting.
func (h *Hub) deliver(msg notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, a := range msg.Accounts {
		for ch := range h.subs[a] {
			select {
			case ch <- msg.Hint:
			default:
			}
		}
	}
}
