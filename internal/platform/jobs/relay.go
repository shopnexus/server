package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// relayLock is the advisory lock key held by the one relay that publishes.
const relayLock = 7_301_003

// Relay publishes outbox rows to JetStream in id order. Several may run; the
// one holding the advisory lock publishes and the others wait for it.
type Relay struct {
	pool *pgxpool.Pool
	js   jetstream.JetStream
	log  *slog.Logger
	poll time.Duration

	leader atomic.Bool
	// afterPublish runs between publish and stamping published_at; tests
	// use it to simulate a crash at the worst moment.
	afterPublish func(id uuid.UUID) error
}

func NewRelay(pool *pgxpool.Pool, js jetstream.JetStream, log *slog.Logger) *Relay {
	return &Relay{pool: pool, js: js, log: log, poll: 2 * time.Second}
}

// IsLeader reports whether this relay holds the lock.
func (r *Relay) IsLeader() bool { return r.leader.Load() }

// Run blocks until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) error {
	for {
		err := r.lead(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			r.log.ErrorContext(ctx, "relay stopped leading", "err", err)
		}
		if !sleep(ctx, time.Second) {
			return ctx.Err()
		}
	}
}

// lead waits for the lock on a dedicated connection, then publishes until ctx
// ends or the connection breaks.
func (r *Relay) lead(ctx context.Context) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	for {
		var got bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", relayLock).Scan(&got); err != nil {
			return err
		}
		if got {
			break
		}
		if !sleep(ctx, time.Second) {
			return nil
		}
	}
	r.leader.Store(true)
	defer func() {
		r.leader.Store(false)
		// The lock and LISTEN belong to the session; clear both before the
		// connection goes back to the pool.
		bg := context.Background()
		conn.Exec(bg, "UNLISTEN *")
		conn.Exec(bg, "SELECT pg_advisory_unlock($1)", relayLock)
	}()

	if _, err := conn.Exec(ctx, "LISTEN outbox"); err != nil {
		return err
	}
	backoff := 100 * time.Millisecond
	lastCleanup := time.Time{}
	for {
		if err := r.drain(ctx, conn.Conn()); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			r.log.WarnContext(ctx, "relay publish failed; retrying", "err", err, "in", backoff)
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, 5*time.Second)
			continue
		}
		backoff = 100 * time.Millisecond

		if time.Since(lastCleanup) > time.Hour {
			r.cleanup(ctx, conn.Conn())
			lastCleanup = time.Now()
		}

		waitCtx, cancel := context.WithTimeout(ctx, r.poll)
		_, err := conn.Conn().WaitForNotification(waitCtx)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
}

type pendingRow struct {
	id       uuid.UUID
	subject  string
	payload  []byte
	headers  map[string]string
	deadline *time.Time
}

const selectPending = `
SELECT id, subject, payload, headers, deadline FROM outbox
WHERE published_at IS NULL ORDER BY id LIMIT 100`

// drain publishes every pending row, one at a time, each acknowledged by
// JetStream before it is stamped. On error it returns without skipping the
// row, so the next attempt starts from the same one and order holds.
func (r *Relay) drain(ctx context.Context, conn *pgx.Conn) error {
	for {
		rows, err := conn.Query(ctx, selectPending)
		if err != nil {
			return err
		}
		batch, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pendingRow, error) {
			var p pendingRow
			err := row.Scan(&p.id, &p.subject, &p.payload, &p.headers, &p.deadline)
			return p, err
		})
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, p := range batch {
			if err := r.publish(ctx, conn, p); err != nil {
				return fmt.Errorf("outbox %s (%s): %w", p.id, p.subject, err)
			}
		}
	}
}

func (r *Relay) publish(ctx context.Context, conn *pgx.Conn, p pendingRow) error {
	msg := nats.NewMsg(p.subject)
	msg.Data = p.payload
	for k, v := range p.headers {
		msg.Header.Set(k, v)
	}
	if p.deadline != nil {
		msg.Header.Set(headerDeadline, p.deadline.UTC().Format(time.RFC3339Nano))
	}
	if _, err := r.js.PublishMsg(ctx, msg, jetstream.WithMsgID(p.id.String())); err != nil {
		return err
	}
	if r.afterPublish != nil {
		if err := r.afterPublish(p.id); err != nil {
			return err
		}
	}
	_, err := conn.Exec(ctx, "UPDATE outbox SET published_at = now() WHERE id = $1", p.id)
	return err
}

// cleanup deletes published rows older than 7 days, 1000 at a time.
func (r *Relay) cleanup(ctx context.Context, conn *pgx.Conn) {
	const q = `DELETE FROM outbox WHERE id IN (
		SELECT id FROM outbox WHERE published_at < now() - interval '7 days' LIMIT 1000)`
	for {
		tag, err := conn.Exec(ctx, q)
		if err != nil {
			r.log.WarnContext(ctx, "outbox cleanup failed", "err", err)
			return
		}
		if tag.RowsAffected() < 1000 {
			return
		}
	}
}

// sleep waits d or until ctx ends; it reports whether d elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
