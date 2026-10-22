package telemetry

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger writes text or JSON records to w at level and above, adds trace_id
// and span_id from the context, and sends each record to the global OTel
// logger provider as well. Before Setup installs one, that provider is a no-op.
func NewLogger(w io.Writer, format string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var local slog.Handler = slog.NewJSONHandler(w, opts)
	if format == "text" {
		local = slog.NewTextHandler(w, opts)
	}
	return slog.New(&contextHandler{
		level: level,
		inner: slog.NewMultiHandler(local, otelslog.NewHandler("server2")),
	})
}

// contextHandler adds the trace and span IDs of the context's span.
type contextHandler struct {
	level slog.Level
	inner slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level && h.inner.Enabled(ctx, l)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r) //nolint:wrapcheck // A slog.Handler wrapper returns the inner handler's error as it is.
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{level: h.level, inner: h.inner.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{level: h.level, inner: h.inner.WithGroup(name)}
}
