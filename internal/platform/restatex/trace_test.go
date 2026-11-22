package restatex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

//nolint:paralleltest // The test installs the global tracer provider.
func TestTraceInvocationsParentsTheHandler(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		tp.Shutdown(context.Background())
	})

	// The SDK takes its parent from traceparent, as this handler does.
	var parent trace.SpanContext
	h := traceInvocations(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx := propagation.TraceContext{}.Extract(context.Background(), propagation.HeaderCarrier(r.Header))
		parent = trace.SpanContextFromContext(ctx)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/invoke/Orders/Place", http.NoBody)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/discover", http.NoBody))
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want the invocation's only", len(spans))
	}
	s := spans[0]
	if s.Name() != "Orders/Place" || s.Parent().SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("span %q parent %s", s.Name(), s.Parent().SpanID())
	}
	if parent.SpanID() != s.SpanContext().SpanID() {
		t.Fatalf("the handler's parent is %s, want the invocation span", parent.SpanID())
	}
}
