package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

type ping struct {
	N int `json:"n"`
}

func (ping) Subject() string { return "cmd.test.ping" }

type outboxRow struct {
	subject   string
	payload   []byte
	headers   map[string]string
	deadline  *time.Time
	published *time.Time
}

func rows(t *testing.T, pool *pgxpool.Pool) []outboxRow {
	t.Helper()
	rs, err := pool.Query(context.Background(),
		"SELECT subject, payload, headers, deadline, published_at FROM outbox ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []outboxRow
	for rs.Next() {
		var r outboxRow
		if err := rs.Scan(&r.subject, &r.payload, &r.headers, &r.deadline, &r.published); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestEnqueueCommitsWithTheTransaction(t *testing.T) {
	pool := pgtest.New(t)
	tp := sdktrace.NewTracerProvider()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	ctx, span := tp.Tracer("t").Start(context.Background(), "request")
	defer span.End()

	err := postgres.NewTxRunner(pool).InTx(ctx, func(q postgres.Querier) error {
		return Enqueue(ctx, q, ping{N: 7})
	})
	if err != nil {
		t.Fatal(err)
	}
	got := rows(t, pool)
	if len(got) != 1 || got[0].subject != "cmd.test.ping" || got[0].published != nil {
		t.Fatalf("rows %+v", got)
	}
	var p ping
	if json.Unmarshal(got[0].payload, &p); p.N != 7 {
		t.Fatalf("payload %s", got[0].payload)
	}
	if tp := got[0].headers["traceparent"]; tp == "" || tp[3:35] != span.SpanContext().TraceID().String() {
		t.Fatalf("traceparent %q", tp)
	}
}

func TestEnqueueRollsBackWithTheTransaction(t *testing.T) {
	pool := pgtest.New(t)
	boom := errors.New("boom")
	err := postgres.NewTxRunner(pool).InTx(context.Background(), func(q postgres.Querier) error {
		if err := Enqueue(context.Background(), q, ping{N: 1}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || len(rows(t, pool)) != 0 {
		t.Fatalf("err %v, rows %d", err, len(rows(t, pool)))
	}
}

func TestEnqueueDeadline(t *testing.T) {
	pool := pgtest.New(t)
	d := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Microsecond)
	if err := Enqueue(context.Background(), pool, ping{N: 1}, Deadline(d)); err != nil {
		t.Fatal(err)
	}
	if got := rows(t, pool); got[0].deadline == nil || !got[0].deadline.Equal(d) {
		t.Fatalf("deadline %v", got[0].deadline)
	}
}

type badSubject struct{}

func (badSubject) Subject() string { return "sms.send" } // no cmd./evt. prefix

type bigPayload struct {
	Blob string `json:"blob"`
}

func (bigPayload) Subject() string { return "cmd.test.big" }

func TestEnqueueRefusesMessagesJetStreamWouldReject(t *testing.T) {
	pool := pgtest.New(t)
	if err := Enqueue(context.Background(), pool, badSubject{}); err == nil {
		t.Error("subject outside cmd.>/evt.> accepted")
	}
	if err := Enqueue(context.Background(), pool, bigPayload{Blob: strings.Repeat("x", 1<<20)}); err == nil {
		t.Error("payload over the NATS limit accepted")
	}
	if n := len(rows(t, pool)); n != 0 {
		t.Fatalf("%d rows written", n)
	}
}
