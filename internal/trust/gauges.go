package trust

import (
	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/telemetry"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// registerGauges reports the open reports and cases and the reviews, for
// the ops dashboard.
func registerGauges(d module.Deps) {
	g := telemetry.CountGauges{Meter: otel.Meter("github.com/shopnexus/server/internal/trust"), Now: d.Now, Log: d.Log}
	g.Gauge("trust.reports.open", "Reports neither withdrawn nor resolved.", repository.NewReports(d.Pool).CountOpen)
	g.Gauge("trust.report_cases.open", "Accounts with an open report case.", repository.NewCases(d.Pool).CountOpen)
	telemetry.GaugeBy(g, "trust.reviews", "Reviews not deleted, by the role of the person reviewed.", "role",
		[]domain.Role{domain.RoleSeller, domain.RoleBuyer}, repository.NewReviews(d.Pool).CountByRole)
}
