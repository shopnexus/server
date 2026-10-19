package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server2/internal/platform/config"
)

// Without an exporter, spans must still carry real ids, so logs, error bodies
// and the Trace-Id header stay correlated.
func TestSetupWithoutEndpointStillMintsTraceIDs(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.Config{ServiceName: "t"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())

	_, span := otel.Tracer("t").Start(context.Background(), "op")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatalf("span context %v is not valid", span.SpanContext())
	}
}
