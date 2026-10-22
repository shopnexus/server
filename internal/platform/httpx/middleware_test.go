package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server2/internal/platform/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// useRecorder installs a span recorder as the global tracer provider.
func useRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
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
	return rec
}

func testMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte("ok"))
	})
	return mux
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func attr(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value, true
		}
	}
	return attribute.Value{}, false
}

//nolint:paralleltest // The test installs the global tracer provider.
func TestTraceNamesSpanByRoutePattern(t *testing.T) {
	rec := useRecorder(t)
	serve(Trace(testMux()), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/42", http.NoBody))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	s := spans[0]
	if s.Name() != "GET /items/{id}" {
		t.Fatalf("span name %q", s.Name())
	}
	if v, _ := attr(s.Attributes(), "http.route"); v.AsString() != "/items/{id}" {
		t.Fatalf("http.route = %v", v)
	}
	if v, _ := attr(s.Attributes(), "http.response.status_code"); v.AsInt64() != 200 {
		t.Fatalf("status = %v", v)
	}
}

//nolint:paralleltest // The test installs the global tracer provider.
func TestTraceRecordsHandlerStatus(t *testing.T) {
	rec := useRecorder(t)
	serve(Trace(testMux()), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/missing", http.NoBody))

	if v, _ := attr(rec.Ended()[0].Attributes(), "http.response.status_code"); v.AsInt64() != 404 {
		t.Fatalf("status = %v", v)
	}
}

//nolint:paralleltest // The test installs the global tracer provider.
func TestTraceContinuesIncomingTrace(t *testing.T) {
	rec := useRecorder(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/42", http.NoBody)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	serve(Trace(testMux()), req)

	s := rec.Ended()[0]
	if got := s.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id %s", got)
	}
	if got := s.Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Fatalf("parent %s", got)
	}
}

//nolint:paralleltest // The test installs the global tracer provider.
func TestTraceUnknownPathHasNoPattern(t *testing.T) {
	rec := useRecorder(t)
	serve(Trace(testMux()), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", http.NoBody))

	s := rec.Ended()[0]
	if s.Name() != "GET" {
		t.Fatalf("span name %q", s.Name())
	}
	if _, ok := attr(s.Attributes(), "http.route"); ok {
		t.Fatal("unexpected http.route")
	}
}

//nolint:paralleltest // The test installs the global tracer provider.
func TestTraceSetsTraceIDHeader(t *testing.T) {
	rec := useRecorder(t)
	resp := serve(Trace(testMux()), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/42", http.NoBody))

	if got := resp.Header().Get("Trace-Id"); got != rec.Ended()[0].SpanContext().TraceID().String() {
		t.Fatalf("Trace-Id %q", got)
	}
}

//nolint:paralleltest // The test installs the global tracer provider.
func TestAccessLogLine(t *testing.T) {
	rec := useRecorder(t)
	var buf bytes.Buffer
	log := telemetry.NewLogger(&buf, "json", slog.LevelInfo)
	serve(Trace(AccessLog(log)(testMux())), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/42", http.NoBody))

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("log %q: %v", buf.String(), err)
	}
	if m["method"] != "GET" || m["route"] != "/items/{id}" || m["status"] != float64(200) {
		t.Fatalf("got %v", m)
	}
	if _, ok := m["duration_ms"]; !ok {
		t.Fatalf("no duration_ms: %v", m)
	}
	if m["trace_id"] != rec.Ended()[0].SpanContext().TraceID().String() {
		t.Fatalf("trace_id %v", m["trace_id"])
	}
}
