// Package httpx holds the HTTP pieces every module shares: middleware, the
// route registry, request decoding and the error format.
package httpx

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/shopnexus/server/internal/platform/telemetry"
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
	return w.ResponseWriter.Write(b) //nolint:wrapcheck // A ResponseWriter wrapper returns the write error as it is.
}

// Unwrap lets [http.ResponseController] reach Flush and deadlines.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Hijack hands the connection to a WebSocket, for a library that asserts
// [http.Hijacker] instead of going through [http.ResponseController].
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack() //nolint:wrapcheck // A ResponseWriter wrapper returns the hijack error as it is.
}

func (w *statusWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

type routeKey struct{}

// noteRoute hands the matched path to the Trace above, which cannot read it
// from r.Pattern once a middleware between them has copied the request.
func noteRoute(r *http.Request, path string) {
	if p, ok := r.Context().Value(routeKey{}).(*string); ok {
		*p = path
	}
}

// route returns the path of the route that matched, or "" when none did.
// Never use r.URL.Path here: one series per ID would follow.
func route(noted string, r *http.Request) string {
	if noted != "" {
		return noted
	}
	if _, path, ok := strings.Cut(r.Pattern, " "); ok {
		return path
	}
	return r.Pattern
}

// Trace opens a server span per request, continues an incoming traceparent,
// names the span by the matched route pattern, records request duration and
// writes one access log line after the request finishes, with the client,
// country and visitor that Visitors noted. It is the outermost middleware,
// so a request that panicked logs the 500 Recover wrote.
func Trace(log *slog.Logger) func(http.Handler) http.Handler {
	tracer := otel.Tracer("github.com/shopnexus/server/internal/platform/httpx")
	duration := telemetry.Must(otel.Meter("github.com/shopnexus/server/internal/platform/httpx").Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
		telemetry.Seconds,
	))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, r.Method, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()
			var noted string
			ctx = context.WithValue(ctx, routeKey{}, &noted)
			ctx, v := withVisit(ctx)

			w.Header().Set("Trace-Id", span.SpanContext().TraceID().String())
			sw := &statusWriter{ResponseWriter: w}
			// A plain mux writes Pattern on the request it receives, so read it from r2.
			r2 := r.WithContext(ctx)
			next.ServeHTTP(sw, r2)

			status, rt, took := sw.code(), route(noted, r2), time.Since(start)
			attrs := []attribute.KeyValue{
				attribute.String("http.request.method", r.Method),
				attribute.Int("http.response.status_code", status),
			}
			if rt != "" {
				span.SetName(r.Method + " " + rt)
				attrs = append(attrs, attribute.String("http.route", rt))
			}
			if v.client != "" {
				attrs = append(attrs, attribute.String("client", v.client))
			}
			span.SetAttributes(attrs...)
			if status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
			duration.Record(ctx, took.Seconds(), metric.WithAttributes(attrs...))
			args := []any{
				"method", r.Method,
				"route", rt,
				"status", status,
				"duration_ms", float64(took.Microseconds()) / 1000,
			}
			if v.client != "" {
				args = append(args, "client", v.client)
			}
			if v.visitor != "" {
				args = append(args, "visitor", v.visitor, "country", v.country)
			}
			if v.bot {
				args = append(args, "ua", v.ua)
			}
			log.InfoContext(ctx, "request", args...)
		})
	}
}
