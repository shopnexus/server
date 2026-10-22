package postgres_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/shopnexus/server2/internal/platform/pgtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

//nolint:paralleltest // The test installs the global tracer provider.
func TestTracerRecordsQuerySpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	url := pgtest.Empty(t).Config().ConnString()
	pool, err := postgres.NewPool(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")
	if _, err := pool.Exec(ctx, "select 1"); err != nil {
		t.Fatal(err)
	}
	parent.End()

	for _, s := range rec.Ended() {
		if s.Name() == "parent" {
			continue
		}
		attrs := map[attribute.Key]string{}
		for _, a := range s.Attributes() {
			attrs[a.Key] = a.Value.String()
		}
		if attrs["db.query.text"] != "select 1" {
			continue
		}
		if attrs["db.system.name"] != "postgresql" {
			t.Fatalf("attrs %v", attrs)
		}
		if s.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("query span is not a child of the request span")
		}
		return
	}
	t.Fatalf("no span for select 1 among %d spans", len(rec.Ended()))
}
