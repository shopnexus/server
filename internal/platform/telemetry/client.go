package telemetry

import (
	"context"
	crand "crypto/rand"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// clientTracers export the spans clients measured, one per client app, each
// under its own service name; Setup fills it when it exports traces.
var clientTracers map[string]*sdktrace.TracerProvider

// clientApps are the apps whose spans the server records.
var clientApps = []string{"android", "website"}

func newClientTracers(exp sdktrace.SpanExporter, env string) map[string]*sdktrace.TracerProvider {
	out := make(map[string]*sdktrace.TracerProvider, len(clientApps))
	for _, app := range clientApps {
		out[app] = sdktrace.NewTracerProvider(
			sdktrace.WithResource(resource.NewSchemaless(semconv.ServiceName(app), semconv.DeploymentEnvironmentName(env))),
			sdktrace.WithBatcher(exp),
			sdktrace.WithIDGenerator(reportedIDs{}),
		)
	}
	return out
}

// RecordClientSpan exports a span a client measured, with the trace and
// span ids it sent in traceparent, so the server's spans of the call hang
// under it. Without trace export, or for another app, it does nothing.
func RecordClientSpan(ctx context.Context, app, name string, traceID trace.TraceID, spanID trace.SpanID,
	start, end time.Time, attrs ...attribute.KeyValue,
) {
	tp, ok := clientTracers[app]
	if !ok {
		return
	}
	ctx = context.WithValue(ctx, reportedKey{}, reported{traceID, spanID})
	_, span := tp.Tracer("github.com/shopnexus/server/internal/platform/telemetry").Start(ctx, name,
		trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindClient), trace.WithTimestamp(start), trace.WithAttributes(attrs...))
	span.End(trace.WithTimestamp(end))
}

type reportedKey struct{}

type reported struct {
	trace trace.TraceID
	span  trace.SpanID
}

// reportedIDs gives a client span the ids the client reported.
type reportedIDs struct{}

func (reportedIDs) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if r, ok := ctx.Value(reportedKey{}).(reported); ok {
		return r.trace, r.span
	}
	var t trace.TraceID
	var s trace.SpanID
	_, _ = crand.Read(t[:])
	_, _ = crand.Read(s[:])
	return t, s
}

func (reportedIDs) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	var s trace.SpanID
	_, _ = crand.Read(s[:])
	return s
}
