package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

//nolint:paralleltest // The test sets the package's client tracers.
func TestRecordClientSpanKeepsTheClientsIDs(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	prev := clientTracers
	clientTracers = newClientTracers(exp, "test")
	t.Cleanup(func() { clientTracers = prev })

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	start := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)
	RecordClientSpan(t.Context(), "android", "GET /v1/search", traceID, spanID, start, start.Add(640*time.Millisecond),
		attribute.String("network", "cellular"))
	RecordClientSpan(t.Context(), "tv", "GET /v1/search", traceID, spanID, start, start, nil...)
	for _, tp := range clientTracers {
		if err := tp.ForceFlush(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want android's only", len(spans))
	}
	s := spans[0]
	if s.SpanContext.TraceID() != traceID || s.SpanContext.SpanID() != spanID || s.Parent.IsValid() {
		t.Fatalf("ids %s/%s parent %v", s.SpanContext.TraceID(), s.SpanContext.SpanID(), s.Parent)
	}
	if s.Name != "GET /v1/search" || !s.StartTime.Equal(start) || s.EndTime.Sub(s.StartTime) != 640*time.Millisecond {
		t.Fatalf("span %q %v..%v", s.Name, s.StartTime, s.EndTime)
	}
	if v, _ := s.Resource.Set().Value("service.name"); v.AsString() != "android" {
		t.Fatalf("service %v", v)
	}
}
