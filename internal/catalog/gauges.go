package catalog

import (
	"context"

	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/telemetry"
)

// registerGauges reports how many listings and drafts there are now, for
// the ops dashboard. Deleted listings are left out.
func registerGauges(d module.Deps) {
	g := telemetry.CountGauges{Meter: otel.Meter("github.com/shopnexus/server/internal/catalog"), Now: d.Now, Log: d.Log}
	statuses := []domain.Status{
		domain.StatusProcessing, domain.StatusActive, domain.StatusHidden, domain.StatusRejected, domain.StatusReserved, domain.StatusSold,
	}
	telemetry.GaugeBy(g, "catalog.listings", "Listings by status.", "status", statuses, repository.NewListings(d.Pool).CountAll)
	drafts := repository.NewDrafts(d.Pool)
	g.Gauge("catalog.listing_drafts", "Listing drafts not expired.", func(ctx context.Context) (int, error) {
		return drafts.CountLive(ctx, d.Now())
	})
}
