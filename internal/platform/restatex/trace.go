package restatex

import (
	"net/http"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/platform/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// traceInvocations opens a server span per handler attempt Restate sends,
// a child of the trace the ingress call carried, and records its duration
// by service and handler. Discovery and health requests pass untraced.
func traceInvocations(next http.Handler) http.Handler {
	const scope = "github.com/shopnexus/server/internal/platform/restatex"
	tracer := otel.Tracer(scope)
	duration := telemetry.Must(otel.Meter(scope).Float64Histogram(
		"restate.invocation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Restate handler attempts."),
		telemetry.Seconds,
	))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, invoke := strings.CutPrefix(r.URL.Path, "/invoke/")
		service, handler, ok := strings.Cut(rest, "/")
		if !invoke || !ok {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		attrs := metric.WithAttributes(attribute.String("restate.service", service), attribute.String("restate.handler", handler))
		carrier := propagation.HeaderCarrier(r.Header)
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), carrier)
		ctx, span := tracer.Start(ctx, service+"/"+handler, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		// The SDK reads the parent from traceparent, so it gets this span.
		otel.GetTextMapPropagator().Inject(ctx, carrier)
		next.ServeHTTP(w, r.WithContext(ctx))
		duration.Record(ctx, time.Since(start).Seconds(), attrs)
	})
}
