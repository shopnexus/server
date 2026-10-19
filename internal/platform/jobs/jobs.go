// Package jobs moves work to the outside world without losing it. Use cases
// Enqueue a message in the same transaction as their change; a single Relay
// publishes the outbox to JetStream in order; Handle runs a typed consumer
// per message subject, with retries and a dead-letter copy. Delivery is at
// least once, so handlers must tolerate duplicates.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/shopnexus/server2/internal/platform/postgres"
)

// Message is a job payload. Its Subject names the JetStream subject, which
// starts with "cmd." for commands and "evt." for events.
type Message interface {
	Subject() string
}

type enqueueOptions struct {
	deadline *time.Time
}

// Option adjusts one Enqueue call.
type Option func(*enqueueOptions)

// Deadline drops the message unhandled once t has passed, for work that is
// useless late, like an OTP that has expired.
func Deadline(t time.Time) Option {
	return func(o *enqueueOptions) { o.deadline = &t }
}

// validSubject is what the CMD and EVT streams accept. A row the relay can
// never publish would stall every row behind it, so Enqueue refuses it.
var validSubject = regexp.MustCompile(`^(cmd|evt)(\.[a-z0-9_]+)+$`)

// maxPayload stays under NATS's default 1 MB max_payload, leaving room for
// headers.
const maxPayload = 900 << 10

const insertOutbox = `INSERT INTO outbox (subject, payload, headers, deadline) VALUES ($1, $2, $3, $4)`

// Enqueue writes m to the outbox on q. Run it inside the transaction that
// makes the change, so the message exists exactly when the change commits.
func Enqueue(ctx context.Context, q postgres.Querier, m Message, opts ...Option) error {
	var o enqueueOptions
	for _, opt := range opts {
		opt(&o)
	}
	if !validSubject.MatchString(m.Subject()) {
		return fmt.Errorf("enqueue: subject %q must look like cmd.x.y or evt.x.y", m.Subject())
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode %s: %w", m.Subject(), err)
	}
	if len(payload) > maxPayload {
		return fmt.Errorf("enqueue %s: payload of %d bytes exceeds %d", m.Subject(), len(payload), maxPayload)
	}
	headers := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, headers)
	_, err = q.Exec(ctx, insertOutbox, m.Subject(), payload, map[string]string(headers), o.deadline)
	return err
}
