package deal

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/repository"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/telemetry"
)

// registerGauges reports how many deals and orders are in each status now,
// for the ops dashboard.
func registerGauges(d module.Deps) {
	g := telemetry.CountGauges{Meter: otel.Meter("github.com/shopnexus/server/internal/deal"), Now: d.Now, Log: d.Log}
	deals := repository.NewDeals(d.Pool)
	telemetry.GaugeBy(g, "deal.deals", "Deals by status.", "status",
		[]domain.Status{domain.StatusNegotiating, domain.StatusAgreed, domain.StatusSold}, deals.CountDeals)
	telemetry.GaugeBy(g, "deal.orders", "Orders by status.", "status",
		[]domain.OrderStatus{domain.OrderPending, domain.OrderShipped, domain.OrderCompleted, domain.OrderFailed, domain.OrderCancelled},
		func(ctx context.Context) (map[domain.OrderStatus]int, error) {
			return deals.CountOrders(ctx, time.Time{})
		})
}
