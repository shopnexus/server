package postgres

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracer opens one client span per query with its SQL text and the rows it
// touched, so a slow query or an N+1 loop shows in the request's trace.
type tracer struct{}

func (tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op := "QUERY"
	if f := strings.Fields(data.SQL); len(f) > 0 {
		op = strings.ToUpper(f[0])
	}
	ctx, _ = otel.Tracer("github.com/shopnexus/server2/internal/platform/postgres").Start(ctx, op,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.operation.name", op),
			attribute.String("db.query.text", data.SQL),
		),
	)
	return ctx
}

func (tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int64("db.response.rows_affected", data.CommandTag.RowsAffected()))
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}
	span.End()
}
