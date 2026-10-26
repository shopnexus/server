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

// Bus delivers events to the handlers subscribed to their kind. It sends
// each event once to its Restate service Events, whose Deliver hands it to
// every subscriber; each subscriber then runs as an invocation of its own,
// so a failing handler blocks no other.
type Bus struct {
	db      *pgxpool.Pool
	ingress *ingress.Client
	prefix  string
	now     func() time.Time
	log     *slog.Logger
	mu      sync.Mutex
	subs    map[Kind][]Subscription
	running bool
}

// NewBus builds a bus whose Events service carries prefix, the process's
// Restate service prefix.
func NewBus(db *pgxpool.Pool, in *ingress.Client, prefix string, now func() time.Time, log *slog.Logger) *Bus {
	return &Bus{db: db, ingress: in, prefix: prefix, now: now, log: log, subs: map[Kind][]Subscription{}}
}

// Definition is the bus's Restate service, Events, which app serves with
// the modules' services.
func (b *Bus) Definition() restate.ServiceDefinition { return restate.Reflect(deliverer{bus: b}) }

func (b *Bus) service() string { return b.prefix + deliverService }

// subscribers returns the subscriptions of kind.
func (b *Bus) subscribers(kind Kind) []Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.subs[kind]
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
// Restate services this process serves, do not have, and defs without the
// bus's own Events service. Deliver would retry such a send forever.
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
	if !handlers[b.service()][deliverHandler] {
		errs = append(errs, fmt.Errorf("events: %s/%s is not served; add Bus.Definition to the Restate services", b.service(), deliverHandler))
	}
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
SELECT id, kind, subject_id, payload, occurred_at FROM events
WHERE dispatched_at IS NULL AND (next_attempt_at IS NULL OR next_attempt_at <= @now)
ORDER BY id LIMIT @limit
FOR UPDATE SKIP LOCKED`

// backOff makes refused events wait 1 s, 2 s, 4 s and so on, up to an hour.
const backOff = `
UPDATE events SET attempts = attempts + 1,
       next_attempt_at = @now::timestamptz + least(interval '1 second' * power(2, attempts), interval '1 hour')
WHERE id = ANY(@ids)`

// dispatch sends one batch to Events.Deliver, one send per event, and
// marks what Restate took, in one transaction, so two buses never hold the
// same event; it returns how many it marked. An event whose kind has no
// subscriber is marked without a send. When the ingress refuses an event
// (4xx), the event backs off and the batch goes on. Any other failed send
// (Restate unreachable, silent past sendTimeout, 5xx) ends the batch.
// Events not marked are sent again later under the same key.
func (b *Bus) dispatch(ctx context.Context) (int, error) {
	b.mu.Lock()
	subs := b.subs
	b.mu.Unlock()
	var (
		marked  int
		sendErr error
	)
	err := pgx.BeginFunc(ctx, b.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, selectPending, pgx.StrictNamedArgs{"now": b.now(), "limit": batchSize})
		if err != nil {
			return fmt.Errorf("select pending events: %w", err)
		}
		pending, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Event])
		if err != nil {
			return fmt.Errorf("read pending events: %w", err)
		}
		sent := make([]uuid.UUID, 0, len(pending))
		var refused []uuid.UUID
		for _, e := range pending {
			if len(subs[e.Kind]) == 0 {
				sent = append(sent, e.ID)
				continue
			}
			sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
			_, err = ingress.ServiceSend[Event](b.ingress, b.service(), deliverHandler).
				Send(sendCtx, e, restate.WithIdempotencyKey(e.ID.String()))
			cancel()
			if ie, ok := errors.AsType[ingress.Error](err); ok && ie.Code() >= 400 && ie.Code() < 500 {
				b.log.ErrorContext(ctx, "events: Restate refused an event", "event_id", e.ID, "kind", e.Kind, "err", err)
				refused = append(refused, e.ID)
				continue
			}
			if err != nil {
				sendErr = err
				break
			}
			dispatched.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", string(e.Kind))))
			sent = append(sent, e.ID)
		}
		if len(refused) > 0 {
			if _, err = tx.Exec(ctx, backOff, pgx.StrictNamedArgs{"now": b.now(), "ids": refused}); err != nil {
				return fmt.Errorf("back off refused events: %w", err)
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
