package jobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/shopnexus/server2/internal/platform/natstest"
)

func consumerSetup(t *testing.T) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	nc, js := natstest.Start(t)
	if err := EnsureStreams(context.Background(), js); err != nil {
		t.Fatal(err)
	}
	return nc, js
}

func publish(t *testing.T, js jetstream.JetStream, data string, header nats.Header) {
	t.Helper()
	msg := &nats.Msg{Subject: "cmd.test.ping", Data: []byte(data), Header: header}
	if _, err := js.PublishMsg(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func handle(t *testing.T, js jetstream.JetStream, opts HandleOptions, fn func(context.Context, ping) error) {
	t.Helper()
	if opts.Durable == "" {
		opts.Durable = "pinger"
	}
	stop, err := Handle(context.Background(), js, discard, opts, fn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
}

func TestHandleDecodesAndAcks(t *testing.T) {
	_, js := consumerSetup(t)
	got := make(chan ping, 1)
	handle(t, js, HandleOptions{}, func(_ context.Context, p ping) error { got <- p; return nil })
	publish(t, js, `{"n":5}`, nil)

	select {
	case p := <-got:
		if p.N != 5 {
			t.Fatalf("got %+v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler not called")
	}
	eventually(t, 5*time.Second, func() bool { return streamMsgs(t, js) == 0 })
}

func TestHandleRetriesWithBackoff(t *testing.T) {
	_, js := consumerSetup(t)
	var calls atomic.Int32
	var times []time.Time
	var mu sync.Mutex
	handle(t, js, HandleOptions{BackOff: []time.Duration{100 * time.Millisecond}, MaxDeliver: 5},
		func(context.Context, ping) error {
			mu.Lock()
			times = append(times, time.Now())
			mu.Unlock()
			if calls.Add(1) < 3 {
				return errors.New("vendor down")
			}
			return nil
		})
	publish(t, js, `{"n":1}`, nil)
	eventually(t, 5*time.Second, func() bool { return calls.Load() == 3 })
	eventually(t, 5*time.Second, func() bool { return streamMsgs(t, js) == 0 })

	mu.Lock()
	defer mu.Unlock()
	if gap := times[1].Sub(times[0]); gap < 90*time.Millisecond {
		t.Fatalf("retried after %v, want about 100ms", gap)
	}
}

func TestHandleMovesExhaustedMessagesToDLQ(t *testing.T) {
	nc, js := consumerSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go RunDLQ(ctx, nc, js, discard)

	var calls atomic.Int32
	handle(t, js, HandleOptions{BackOff: []time.Duration{20 * time.Millisecond}, MaxDeliver: 3},
		func(context.Context, ping) error { calls.Add(1); return errors.New("always fails") })
	time.Sleep(100 * time.Millisecond) // DLQ subscription in place
	publish(t, js, `{"n":4}`, nil)

	dlq, _ := js.Stream(context.Background(), "DLQ")
	eventually(t, 10*time.Second, func() bool {
		info, _ := dlq.Info(context.Background())
		return info != nil && info.State.Msgs == 1
	})
	m, err := dlq.GetMsg(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "dlq.cmd.test.ping" || string(m.Data) != `{"n":4}` || m.Header.Get("Dlq-Consumer") != "pinger" {
		t.Fatalf("dlq message %s %s %v", m.Subject, m.Data, m.Header)
	}
	if calls.Load() != 3 {
		t.Fatalf("handler ran %d times, want 3", calls.Load())
	}
	eventually(t, 5*time.Second, func() bool { return streamMsgs(t, js) == 0 })
}

func TestHandleSkipsExpiredMessages(t *testing.T) {
	_, js := consumerSetup(t)
	var calls atomic.Int32
	handle(t, js, HandleOptions{}, func(context.Context, ping) error { calls.Add(1); return nil })
	publish(t, js, `{"n":1}`, nats.Header{"Deadline": []string{time.Now().Add(-time.Minute).Format(time.RFC3339Nano)}})

	eventually(t, 5*time.Second, func() bool { return streamMsgs(t, js) == 0 })
	if calls.Load() != 0 {
		t.Fatal("handler ran for an expired message")
	}
}

func TestHandleTerminatesUndecodableMessages(t *testing.T) {
	_, js := consumerSetup(t)
	var calls atomic.Int32
	handle(t, js, HandleOptions{BackOff: []time.Duration{10 * time.Millisecond}, MaxDeliver: 5},
		func(context.Context, ping) error { calls.Add(1); return nil })
	publish(t, js, `not json`, nil)

	eventually(t, 5*time.Second, func() bool { return streamMsgs(t, js) == 0 })
	if calls.Load() != 0 {
		t.Fatal("handler ran for garbage")
	}
}

func TestHandleConcurrencyLimit(t *testing.T) {
	_, js := consumerSetup(t)
	var running, peak, done atomic.Int32
	handle(t, js, HandleOptions{Concurrency: 2}, func(context.Context, ping) error {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		running.Add(-1)
		done.Add(1)
		return nil
	})
	for range 10 {
		publish(t, js, `{"n":1}`, nil)
	}
	eventually(t, 10*time.Second, func() bool { return done.Load() == 10 })
	if peak.Load() != 2 {
		t.Fatalf("peak concurrency %d, want 2", peak.Load())
	}
}

func TestHandleContinuesTheEnqueueTrace(t *testing.T) {
	_, js := consumerSetup(t)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

	done := make(chan struct{})
	handle(t, js, HandleOptions{}, func(context.Context, ping) error { close(done); return nil })
	// The relay copies outbox headers as stored: lower-case "traceparent".
	publish(t, js, `{"n":1}`, nats.Header{"traceparent": []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}})
	<-done

	eventually(t, 5*time.Second, func() bool { return len(rec.Ended()) == 1 })
	if got := rec.Ended()[0].SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("consumer span trace %s; the enqueue trace was lost", got)
	}
}
