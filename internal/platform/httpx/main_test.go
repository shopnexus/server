package httpx

import (
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestMain installs an SDK tracer provider, so spans carry real trace IDs in
// every test. A test that reads the spans swaps in a recorder with
// useRecorder and runs serially.
func TestMain(m *testing.M) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.TraceContext{})
	os.Exit(m.Run())
}
