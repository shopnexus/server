package events

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/platform/telemetry"
)

// dispatched counts events handed to Restate, by kind.
var dispatched = telemetry.Must(otel.Meter("github.com/shopnexus/server2/internal/platform/events").
	Int64Counter("events_dispatched_total", metric.WithDescription("events handed to Restate, by kind")))
