// Package service holds module beacon's one use case: record what a client
// reports, as metrics and log lines. It keeps no rows.
package service

import (
	"context"
	"log/slog"
	"strconv"
	"time"
	"uuid"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/shopnexus/server/internal/platform/telemetry"
)

// Service is module beacon's contract.
type Service interface {
	// Record turns a batch of client events into metrics and log lines.
	Record(ctx context.Context, b Batch)
}

// Batch is the body of POST /v1/client-events.
type Batch struct {
	App    string  `json:"app"`
	Events []Event `json:"events"`
}

// Event is one client event. Type picks the fields it carries; the spec
// has checked each type's fields.
type Event struct {
	Type string `json:"type"`
	// error
	Message string `json:"message,omitempty"`
	Stack   string `json:"stack,omitempty"`
	// error and vital
	Page string `json:"page,omitempty"`
	// vital
	Name  string  `json:"name,omitempty"`
	Value float64 `json:"value,omitempty"`
	// request
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
	Status     int       `json:"status,omitempty"`
	DurationMs float64   `json:"duration_ms,omitempty"`
	Network    string    `json:"network,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
	SpanID     string    `json:"span_id,omitempty"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	// search_impression and search_click
	SearchID   uuid.UUID   `json:"search_id,omitzero"`
	ListingIDs []uuid.UUID `json:"listing_ids,omitempty"`
	Offset     int         `json:"offset,omitempty"`
	ListingID  uuid.UUID   `json:"listing_id,omitzero"`
	Position   int         `json:"position,omitempty"`
}

// slowRequestMs is how long a client request takes before its log line is
// written; every request is in the metric.
const slowRequestMs = 3000

// A client span must start within this window of the server's clock; a
// phone's clock can be off, and an old report is useless in a trace.
const (
	spanMaxAge  = time.Hour
	spanMaxSkew = 5 * time.Minute
)

// Deps is what New builds Beacon from.
type Deps struct {
	// Route is the route pattern that serves method and path, "" for none.
	Route func(method, path string) string
	Now   func() time.Time
	Log   *slog.Logger
}

// Beacon implements Service in this process.
type Beacon struct {
	route       func(method, path string) string
	now         func() time.Time
	log         *slog.Logger
	errors      metric.Int64Counter
	vitals      metric.Float64Histogram
	shifts      metric.Float64Histogram
	requests    metric.Float64Histogram
	impressions metric.Int64Counter
	clicks      metric.Int64Counter
}

var _ Service = (*Beacon)(nil)

func New(d Deps) *Beacon {
	m := otel.Meter("github.com/shopnexus/server/internal/beacon")
	return &Beacon{
		route: d.Route,
		now:   d.Now,
		log:   d.Log,
		errors: telemetry.Must(m.Int64Counter("client.errors",
			metric.WithDescription("Script errors clients reported, by app."))),
		vitals: telemetry.Must(m.Float64Histogram("client.web_vital.duration", metric.WithUnit("s"),
			metric.WithDescription("LCP, INP, FCP and TTFB of website page views."), telemetry.Seconds)),
		shifts: telemetry.Must(m.Float64Histogram("client.web_vital.cls",
			metric.WithDescription("Cumulative layout shift of website page views."),
			metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.15, 0.25, 0.5, 1))),
		requests: telemetry.Must(m.Float64Histogram("client.request.duration", metric.WithUnit("s"),
			metric.WithDescription("API calls as clients timed them, network included."), telemetry.Seconds)),
		impressions: telemetry.Must(m.Int64Counter("client.search.impressions",
			metric.WithDescription("Search results that came into view, by position."))),
		clicks: telemetry.Must(m.Int64Counter("client.search.clicks",
			metric.WithDescription("Search results opened, by position."))),
	}
}

func (b *Beacon) Record(ctx context.Context, batch Batch) {
	app := attribute.String("app", batch.App)
	for i := range batch.Events {
		e := &batch.Events[i]
		switch e.Type {
		case "error":
			b.errors.Add(ctx, 1, metric.WithAttributes(app))
			b.log.InfoContext(ctx, "client error", "app", batch.App, "page", e.Page, "error", e.Message, "stack", e.Stack)
		case "vital":
			attrs := metric.WithAttributes(app, attribute.String("name", e.Name))
			if e.Name == "CLS" {
				b.shifts.Record(ctx, e.Value, attrs)
			} else {
				b.vitals.Record(ctx, e.Value/1000, attrs)
			}
		case "request":
			route := b.route(e.Method, e.Path)
			if route == "" {
				route = "other"
			}
			attrs := make([]attribute.KeyValue, 0, 5)
			attrs = append(attrs,
				attribute.String("http.route", route),
				attribute.String("http.request.method", e.Method),
				attribute.Int("http.response.status_code", e.Status),
				attribute.String("network", e.Network),
			)
			b.requests.Record(ctx, e.DurationMs/1000, metric.WithAttributes(append(attrs, app)...))
			traceID, traceErr := trace.TraceIDFromHex(e.TraceID)
			spanID, spanErr := trace.SpanIDFromHex(e.SpanID)
			if age := b.now().Sub(e.StartedAt); traceErr == nil && spanErr == nil && age < spanMaxAge && age > -spanMaxSkew {
				end := e.StartedAt.Add(time.Duration(e.DurationMs * float64(time.Millisecond)))
				telemetry.RecordClientSpan(ctx, batch.App, e.Method+" "+route, traceID, spanID, e.StartedAt, end, attrs...)
			}
			if e.Status == 0 || e.Status >= 500 || e.DurationMs >= slowRequestMs {
				b.log.InfoContext(ctx, "client request", "app", batch.App, "method", e.Method, "route", route,
					"status", e.Status, "duration_ms", e.DurationMs, "network", e.Network, "server_trace_id", e.TraceID)
			}
		case "search_impression":
			for j := range e.ListingIDs {
				b.impressions.Add(ctx, 1, metric.WithAttributes(app, attribute.String("position", positionBucket(e.Offset+j))))
			}
			b.log.InfoContext(ctx, "search impression", "app", batch.App, "search_id", e.SearchID,
				"offset", e.Offset, "listing_ids", e.ListingIDs)
		case "search_click":
			b.clicks.Add(ctx, 1, metric.WithAttributes(app, attribute.String("position", positionBucket(e.Position))))
			b.log.InfoContext(ctx, "search click", "app", batch.App, "search_id", e.SearchID,
				"listing_id", e.ListingID, "position", e.Position)
		default:
		}
	}
}

// positionBucket groups a result's position from 0 into a few ranges, so
// the click and impression counters stay small.
func positionBucket(p int) string {
	switch {
	case p < 3:
		return strconv.Itoa(p + 1)
	case p < 5:
		return "4-5"
	case p < 10:
		return "6-10"
	case p < 20:
		return "11-20"
	default:
		return "21+"
	}
}
