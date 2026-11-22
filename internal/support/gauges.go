package support

import (
	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/telemetry"
	"github.com/shopnexus/server/internal/support/repository"
)

// registerGauges reports the open tickets, for the ops dashboard.
func registerGauges(d module.Deps) {
	g := telemetry.CountGauges{Meter: otel.Meter("github.com/shopnexus/server/internal/support"), Now: d.Now, Log: d.Log}
	g.Gauge("support.tickets.open", "Support tickets not resolved.", repository.NewTickets(d.Pool).CountOpen)
}
