package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// headerDeadline carries Enqueue's Deadline as RFC 3339.
const headerDeadline = "Deadline"

// maxInFlight bounds messages a consumer holds unacknowledged, retries
// waiting out their backoff included.
const maxInFlight = 10_000

// HandleOptions configures one consumer. Zero values take the defaults.
type HandleOptions struct {
	Durable     string          // consumer name, e.g. "sms-sender"; required
	Concurrency int             // messages handled at once; default 8
	BackOff     []time.Duration // wait before each retry; default 1s, 4s, 16s, 64s
	MaxDeliver  int             // attempts before the message goes to the DLQ; default 5
}

func (o *HandleOptions) defaults() {
	if o.Concurrency <= 0 {
		o.Concurrency = 8
	}
	if len(o.BackOff) == 0 {
		o.BackOff = []time.Duration{time.Second, 4 * time.Second, 16 * time.Second, 64 * time.Second}
	}
	if o.MaxDeliver <= 0 {
		o.MaxDeliver = 5
	}
}

// Handle runs fn for every message on M's subject. A nil return acknowledges
// the message; an error retries it after the next BackOff step until
// MaxDeliver attempts, after which RunDLQ copies it to the DLQ. A message
// past its Deadline is acknowledged without running fn, and one that does
// not decode into M is terminated. The returned stop waits for running
// handlers.
func Handle[M Message](ctx context.Context, js jetstream.JetStream, log *slog.Logger, opts HandleOptions, fn func(context.Context, M) error) (stop func(), err error) {
	opts.defaults()
	var zero M
	subject := zero.Subject()

	cons, err := js.CreateOrUpdateConsumer(ctx, streamFor(subject), jetstream.ConsumerConfig{
		Durable:       opts.Durable,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		// AckWait covers a handler that crashes and never answers.
		AckWait: max(opts.BackOff[0], 30*time.Second),
		// JetStream counts a message waiting out a NakWithDelay as pending,
		// so a cap equal to Concurrency would let failing messages block new
		// ones. The semaphore below limits how many run at once.
		MaxAckPending: maxInFlight,
		MaxDeliver:    opts.MaxDeliver,
	})
	if err != nil {
		return nil, err
	}

	tracer := otel.Tracer("github.com/shopnexus/server2/internal/platform/jobs")
	slots := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup

	handleOne := func(msg jetstream.Msg) {
		meta, err := msg.Metadata()
		if err != nil {
			log.Error("job without metadata", "subject", subject, "err", err)
			msg.Term()
			return
		}
		ctx := otel.GetTextMapPropagator().Extract(context.Background(), natsCarrier(msg.Headers()))
		ctx, span := tracer.Start(ctx, "process "+subject, trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				attribute.String("messaging.system", "nats"),
				attribute.String("messaging.destination.name", subject),
				attribute.Int64("messaging.nats.delivered", int64(meta.NumDelivered)),
			))
		defer span.End()

		if d := msg.Headers().Get(headerDeadline); d != "" {
			if t, err := time.Parse(time.RFC3339Nano, d); err == nil && time.Now().After(t) {
				log.InfoContext(ctx, "job past its deadline; dropped", "subject", subject)
				msg.Ack()
				return
			}
		}

		var m M
		if err := json.Unmarshal(msg.Data(), &m); err != nil {
			log.ErrorContext(ctx, "job does not decode; terminated", "subject", subject, "err", err)
			span.SetStatus(codes.Error, "undecodable")
			msg.Term()
			return
		}

		if err := callSafely(ctx, fn, m); err != nil {
			step := min(int(meta.NumDelivered), len(opts.BackOff)) - 1
			log.WarnContext(ctx, "job failed", "subject", subject, "attempt", meta.NumDelivered, "err", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			msg.NakWithDelay(opts.BackOff[step])
			return
		}
		msg.Ack()
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			handleOne(msg)
		})
	}, jetstream.PullMaxMessages(opts.Concurrency))
	if err != nil {
		return nil, err
	}
	return func() { cc.Stop(); wg.Wait() }, nil
}

// natsCarrier reads trace headers from a NATS message. NATS keeps header keys
// as written ("traceparent"), while propagation.HeaderCarrier is an
// http.Header and looks up the canonical form ("Traceparent").
type natsCarrier nats.Header

func (c natsCarrier) Get(key string) string { return nats.Header(c).Get(key) }

func (c natsCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }

func (c natsCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// callSafely runs fn and turns a panic into an error, so one bad message is
// retried and dead-lettered instead of taking the process down.
func callSafely[M any](ctx context.Context, fn func(context.Context, M) error, m M) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("handler panic: %v\n%s", v, debug.Stack())
		}
	}()
	return fn(ctx, m)
}
