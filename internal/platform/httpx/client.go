package httpx

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/shopnexus/server/internal/platform/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// scope names this package's tracer and meter.
const scope = "github.com/shopnexus/server/internal/platform/httpx"

// NewClient is an HTTP client whose calls are traced, for every call this
// process makes to another service. A zero timeout leaves the context's.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: defaultTransport(), Timeout: timeout}
}

// defaultTransport is [http.DefaultTransport] traced, built once so every
// client shares its connection pool and instruments.
var defaultTransport = sync.OnceValue(func() http.RoundTripper { return Transport(http.DefaultTransport) })

// Transport opens a client span per request on base, sends the trace on in
// traceparent and records the time to the response headers by host.
func Transport(base http.RoundTripper) http.RoundTripper {
	return &tracedTransport{
		base:   base,
		tracer: otel.Tracer(scope),
		duration: telemetry.Must(otel.Meter(scope).Float64Histogram(
			"http.client.request.duration",
			metric.WithUnit("s"),
			metric.WithDescription("Duration of HTTP client requests."),
			telemetry.Seconds,
		)),
	}
}

type tracedTransport struct {
	base     http.RoundTripper
	tracer   trace.Tracer
	duration metric.Float64Histogram
}

func (t *tracedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	attrs := make([]attribute.KeyValue, 0, 3)
	attrs = append(attrs,
		attribute.String("http.request.method", r.Method),
		attribute.String("server.address", r.URL.Hostname()),
	)
	// The span is named by host: a path can hold IDs, one name each.
	ctx, span := t.tracer.Start(r.Context(), r.Method+" "+r.URL.Host,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(append(attrs, attribute.String("url.path", r.URL.Path))...),
	)
	defer span.End()

	// A RoundTripper must not change the request it was given.
	r = r.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(r.Header))
	res, err := t.base.RoundTrip(r)
	var outcome attribute.KeyValue
	if err != nil {
		var timeout interface{ Timeout() bool }
		switch {
		case errors.As(err, &timeout) && timeout.Timeout():
			outcome = attribute.String("error.type", "timeout")
		case errors.Is(err, context.Canceled):
			outcome = attribute.String("error.type", "canceled")
		default:
			outcome = attribute.String("error.type", "transport")
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		outcome = attribute.Int("http.response.status_code", res.StatusCode)
		if res.StatusCode >= http.StatusBadRequest {
			span.SetStatus(codes.Error, http.StatusText(res.StatusCode))
		}
	}
	span.SetAttributes(outcome)
	t.duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(append(attrs, outcome)...))
	return res, err //nolint:wrapcheck // A RoundTripper returns the transport's error as it is.
}
