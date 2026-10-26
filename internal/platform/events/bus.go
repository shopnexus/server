package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	batchSize = 100
	pollEvery = time.Second
	// sendTimeout bounds one send, so a Restate that stops answering ends
	// the round instead of holding its transaction open.
	sendTimeout = 5 * time.Second
	keepFor     = 7 * 24 * time.Hour
	cleanEvery  = time.Hour
	deleteBatch = 10_000
)

// Subscription hands every event of Kind to a Restate handler.
type Subscription struct {
	Kind    Kind
	Service string // with the service prefix
	Handler string
}

// key is the idempotency key of one delivery; Restate drops a repeat.
func (s Subscription) key(e Event) string { return e.ID.String() + "/" + s.Service + "/" + s.Handler }

// Bus delivers events to the handlers subscribed to their kind. Each event
// to each subscriber is its own Restate invocation, so a failing handler
// blocks no other event.
type Bus struct {
	db      *pgxpool.Pool
	ingress *ingress.Client
	now     func() time.Time
	log     *slog.Logger
	mu      sync.Mutex
	subs    map[Kind][]Subscription
	running bool
}

func NewBus(db *pgxpool.Pool, in *ingress.Client, now func() time.Time, log *slog.Logger) *Bus {
	return &Bus{db: db, ingress: in, now: now, log: log, subs: map[Kind][]Subscription{}}
}

// Subscribe hands every event of kind to service's handler, a Restate
// handler func(restate.Context, events.Event) error; service carries the
// prefix. Modules subscribe in Mount, before Run starts, and Subscribe
// panics after.
func (b *Bus) Subscribe(kind Kind, service, handler string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		panic("events: Subscribe after Run")
	}
	b.subs[kind] = append(b.subs[kind], Subscription{Kind: kind, Service: service, Handler: handler})
}

// Check reports a subscription to a service or handler that defs, the
// Restate services this process serves, do not have; Restate would refuse
// every delivery to it.
func (b *Bus) Check(defs []restate.ServiceDefinition) error {
	handlers := map[string]map[string]bool{}
	for _, d := range defs {
		hs := map[string]bool{}
		for name := range d.Handlers() {
			hs[name] = true
		}
		handlers[d.Name()] = hs
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var errs []error
	for _, subs := range b.subs {
		for _, s := range subs {
			if !handlers[s.Service][s.Handler] {
				errs = append(errs, fmt.Errorf("events: %s subscribes %s/%s, which this process does not serve", s.Kind, s.Service, s.Handler))
			}
		}
	}
	return errors.Join(errs...)
}

// Run delivers pending events, then waits for a notification or the next
// poll, until ctx is done; it returns nil then, after its listen connection
// has closed. While Restate cannot be reached, events stay pending and Run
// keeps trying.
func (b *Bus) Run(ctx context.Context) error {
	b.mu.Lock()
	b.running = true
	b.mu.Unlock()
	wake := make(chan struct{}, 1)
	var listening sync.WaitGroup
	listening.Go(func() { b.listen(ctx, wake) })
	defer listening.Wait()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	clean := time.NewTicker(cleanEvery)
	defer clean.Stop()
	b.deleteDispatched(ctx)
	for {
		for {
			marked, err := b.dispatch(ctx)
			if err != nil {
				if ctx.Err() == nil {
					b.log.WarnContext(ctx, "events: dispatch", "err", err)
				}
				break
			}
			if marked < batchSize {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-clean.C:
			b.deleteDispatched(ctx)
		case <-tick.C:
		case <-wake:
		}
	}
}

// deleteDispatched deletes events dispatched more than keepFor ago, in
// batches, so the first run after a long stop does not lock the table.
func (b *Bus) deleteDispatched(ctx context.Context) {
	for {
		tag, err := b.db.Exec(ctx, `
DELETE FROM events WHERE id IN (
    SELECT id FROM events WHERE dispatched_at < @before LIMIT @limit)`,
			pgx.StrictNamedArgs{"before": b.now().Add(-keepFor), "limit": deleteBatch})
		if err != nil {
			if ctx.Err() == nil {
				b.log.WarnContext(ctx, "events: delete dispatched", "err", err)
			}
			return
		}
		if tag.RowsAffected() < deleteBatch {
			return
		}
	}
}

// listen wakes Run on every notification. A lost connection is listened
// again after a pause; the poll covers the gap.
func (b *Bus) listen(ctx context.Context, wake chan<- struct{}) {
	for ctx.Err() == nil {
		if err := b.waitNotifications(ctx, wake); err != nil && ctx.Err() == nil {
			b.log.WarnContext(ctx, "events: listen", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(pollEvery):
			}
		}
	}
}

// waitNotifications listens on a connection of its own, outside the pool:
// a pooled one would hold a slot for good, and Pool.Close would wait for it.
func (b *Bus) waitNotifications(ctx context.Context, wake chan<- struct{}) error {
	conn, err := pgx.ConnectConfig(ctx, b.db.Config().ConnConfig.Copy())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() {
		// Closing a listen connection that already failed reports that
		// failure again; listen has it from WaitForNotification.
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err = conn.Exec(ctx, "LISTEN events"); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	for {
		if _, err = conn.WaitForNotification(ctx); err != nil {
			return fmt.Errorf("wait for notification: %w", err)
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

const selectPending = `
SELECT id, kind, subject_id, data, at FROM events
WHERE dispatched_at IS NULL ORDER BY id LIMIT @limit
FOR UPDATE SKIP LOCKED`

// dispatch sends one batch and marks what it sent, in one transaction, so
// two buses never hold the same event, and returns how many it marked.
// Restate refusing one delivery (4xx: a service or handler it does not
// know) leaves that event pending and goes on with the next. Any other
// failed send (Restate unreachable, silent past sendTimeout, 5xx) ends the
// batch. Events not marked are sent again next round under the same keys.
func (b *Bus) dispatch(ctx context.Context) (int, error) {
	b.mu.Lock()
	subs := b.subs
	b.mu.Unlock()
	var (
		marked  int
		sendErr error
	)
	err := pgx.BeginFunc(ctx, b.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, selectPending, pgx.StrictNamedArgs{"limit": batchSize})
		if err != nil {
			return fmt.Errorf("select pending events: %w", err)
		}
		pending, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Event])
		if err != nil {
			return fmt.Errorf("read pending events: %w", err)
		}
		sent := make([]uuid.UUID, 0, len(pending))
	batch:
		for _, e := range pending {
			refused := false
			for _, s := range subs[e.Kind] {
				sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
				_, err = ingress.ServiceSend[Event](b.ingress, s.Service, s.Handler).
					Send(sendCtx, e, restate.WithIdempotencyKey(s.key(e)))
				cancel()
				if ie, ok := errors.AsType[ingress.Error](err); ok && ie.Code() >= 400 && ie.Code() < 500 {
					b.log.ErrorContext(ctx, "events: Restate refused a delivery", "event_id", e.ID, "kind", e.Kind,
						"service", s.Service, "handler", s.Handler, "err", err)
					refused = true
					continue
				}
				if err != nil {
					sendErr = err
					break batch
				}
				dispatched.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", string(e.Kind))))
			}
			if !refused {
				sent = append(sent, e.ID)
			}
		}
		if len(sent) == 0 {
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE events SET dispatched_at = @at WHERE id = ANY(@ids)`,
			pgx.StrictNamedArgs{"at": b.now(), "ids": sent}); err != nil {
			return fmt.Errorf("mark events dispatched: %w", err)
		}
		marked = len(sent)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("dispatch events: %w", err)
	}
	if sendErr != nil {
		return marked, fmt.Errorf("send event: %w", sendErr)
	}
	return marked, nil
}
