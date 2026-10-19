// Package httpx holds the HTTP pieces every module shares: middleware, the
// route registry, request decoding and the error format.
package httpx

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// statusWriter remembers the status code a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach Flush and deadlines.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// route returns the path part of the pattern the mux matched, or "" when no
// route matched. Never use r.URL.Path here: one series per ID would follow.
func route(r *http.Request) string {
	if _, path, ok := strings.Cut(r.Pattern, " "); ok {
		return path
	}
	return r.Pattern
}

// Trace opens a server span per request, continues an incoming traceparent,
// names the span by the matched route pattern and records request duration.
func Trace(next http.Handler) http.Handler {
	tracer := otel.Tracer("github.com/shopnexus/server2/internal/platform/httpx")
	duration, _ := otel.Meter("github.com/shopnexus/server2/internal/platform/httpx").Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		w.Header().Set("Trace-Id", span.SpanContext().TraceID().String())
		sw := &statusWriter{ResponseWriter: w}
		// The mux writes Pattern on the request it receives, so read it from r2.
		r2 := r.WithContext(ctx)
		next.ServeHTTP(sw, r2)

		status := sw.code()
		attrs := []attribute.KeyValue{
			attribute.String("http.request.method", r.Method),
			attribute.Int("http.response.status_code", status),
		}
		if rt := route(r2); rt != "" {
			span.SetName(r.Method + " " + rt)
			attrs = append(attrs, attribute.String("http.route", rt))
		}
		span.SetAttributes(attrs...)
		if status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	})
}

// AccessLog writes one line per request after it finishes.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			log.InfoContext(r.Context(), "request",
				"method", r.Method,
				"route", route(r),
				"status", sw.code(),
				"duration_ms", float64(time.Since(start).Microseconds())/1000,
			)
		})
	}
}
