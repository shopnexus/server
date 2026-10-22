package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func spanContext(t *testing.T) (context.Context, trace.SpanContext) {
	t.Helper()
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	t.Cleanup(func() { span.End() })
	return ctx, span.SpanContext()
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("log line %q: %v", buf.String(), err)
	}
	return m
}

func TestLoggerAddsTraceIDs(t *testing.T) {
	t.Parallel()
	ctx, sc := spanContext(t)
	var buf bytes.Buffer
	NewLogger(&buf, "json", slog.LevelInfo).InfoContext(ctx, "x")

	m := decode(t, &buf)
	if m["trace_id"] != sc.TraceID().String() || m["span_id"] != sc.SpanID().String() {
		t.Fatalf("got %v, want trace %s span %s", m, sc.TraceID(), sc.SpanID())
	}
}

func TestLoggerWithoutSpanHasNoTraceID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	NewLogger(&buf, "json", slog.LevelInfo).Info("x")

	if _, ok := decode(t, &buf)["trace_id"]; ok {
		t.Fatal("unexpected trace_id")
	}
}

func TestLoggerWithAttrsKeepsTraceIDs(t *testing.T) {
	t.Parallel()
	ctx, sc := spanContext(t)
	var buf bytes.Buffer
	NewLogger(&buf, "json", slog.LevelInfo).With("k", "v").InfoContext(ctx, "x", "a", 1)

	m := decode(t, &buf)
	if m["trace_id"] != sc.TraceID().String() || m["k"] != "v" {
		t.Fatalf("got %v", m)
	}
}

func TestLoggerRespectsLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	NewLogger(&buf, "json", slog.LevelWarn).Info("x")
	if buf.Len() != 0 {
		t.Fatalf("wrote %q", buf.String())
	}
}

func TestLoggerTextFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	NewLogger(&buf, "text", slog.LevelInfo).Info("hello")
	if !bytes.Contains(buf.Bytes(), []byte("msg=hello")) {
		t.Fatalf("got %q", buf.String())
	}
}
