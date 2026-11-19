// Package relay carries short-lived messages about one key, such as the
// text of a read as the model writes it, from the replica that writes them
// to the replicas whose clients watch that key. A message is the whole
// state so far, so a watcher that misses one loses nothing the next one
// does not carry.
package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/platform/postgres"
)

// channel is the Postgres channel messages travel on.
const channel = "relay"

// MaxMessage is the largest message Publish sends; pg_notify takes under
// 8000 bytes with the key around it.
const MaxMessage = 7800

// Relay publishes on Postgres channel relay and hands what it hears to the
// subscriptions of each key.
type Relay struct {
	db   *pgxpool.Pool
	log  *slog.Logger
	mu   sync.Mutex
	subs map[uuid.UUID]map[*Sub]struct{}
	// closed ends every watch, for a server shutting down.
	closed    chan struct{}
	closeOnce sync.Once
}

func New(db *pgxpool.Pool, log *slog.Logger) *Relay {
	return &Relay{db: db, log: log, subs: map[uuid.UUID]map[*Sub]struct{}{}, closed: make(chan struct{})}
}

// Close tells every watcher to stop, through Sub.Closed.
func (r *Relay) Close() { r.closeOnce.Do(func() { close(r.closed) }) }

// Sub is one watcher of a key.
type Sub struct {
	r   *Relay
	key uuid.UUID
	c   chan json.RawMessage
}

// C delivers the key's messages, the latest when the watcher is slow. A
// nil message means some may have been lost: the watcher reads the state
// again.
func (s *Sub) C() <-chan json.RawMessage { return s.c }

// Closed is closed when the relay closes.
func (s *Sub) Closed() <-chan struct{} { return s.r.closed }

// Close ends the subscription.
func (s *Sub) Close() {
	s.r.mu.Lock()
	defer s.r.mu.Unlock()
	delete(s.r.subs[s.key], s)
	if len(s.r.subs[s.key]) == 0 {
		delete(s.r.subs, s.key)
	}
}

// Subscribe watches key until Close.
func (r *Relay) Subscribe(key uuid.UUID) *Sub {
	s := &Sub{r: r, key: key, c: make(chan json.RawMessage, 1)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subs[key] == nil {
		r.subs[key] = map[*Sub]struct{}{}
	}
	r.subs[key][s] = struct{}{}
	return s
}

type note struct {
	Key uuid.UUID       `json:"key"`
	Msg json.RawMessage `json:"msg"`
}

// Publish sends msg about key to every replica. A message over MaxMessage
// is an error.
func (r *Relay) Publish(ctx context.Context, key uuid.UUID, msg json.RawMessage) error {
	if len(msg) > MaxMessage {
		return fmt.Errorf("relay message of %d bytes", len(msg))
	}
	payload, err := json.Marshal(note{Key: key, Msg: msg})
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	if _, err = r.db.Exec(ctx, `SELECT pg_notify(@channel, @payload)`,
		pgx.StrictNamedArgs{"channel": channel, "payload": string(payload)}); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	return nil
}

// Run listens until ctx is done. Each time it starts to listen it sends
// every watcher a nil message, since what was sent before is lost.
func (r *Relay) Run(ctx context.Context) {
	postgres.Listen(ctx, r.db, channel, r.log, func() { r.deliverAll(nil) }, func(payload string) {
		var n note
		if err := json.Unmarshal([]byte(payload), &n); err != nil {
			r.log.WarnContext(ctx, "relay: bad message", "err", err)
			return
		}
		r.deliver(n.Key, n.Msg)
	})
}

func (r *Relay) deliver(key uuid.UUID, msg json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for s := range r.subs[key] {
		s.put(msg)
	}
}

func (r *Relay) deliverAll(msg json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, subs := range r.subs {
		for s := range subs {
			s.put(msg)
		}
	}
}

// put replaces a message the watcher has not taken yet.
func (s *Sub) put(msg json.RawMessage) {
	select {
	case <-s.c:
	default:
	}
	s.c <- msg
}
