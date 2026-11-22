package account

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/telemetry"
)

// registerGauges reports how many accounts there are now and how many have
// a verified phone, for the ops dashboard. Deleted accounts and ShopNexus's
// own are left out.
func registerGauges(d module.Deps) {
	g := telemetry.CountGauges{Meter: otel.Meter("github.com/shopnexus/server/internal/account"), Now: d.Now, Log: d.Log}
	accounts := repository.NewAccounts(d.Pool)
	telemetry.GaugeBy(g, "account.accounts", "Accounts by status; a suspension that ended counts as active.", "status",
		[]domain.Status{domain.StatusActive, domain.StatusSuspended},
		func(ctx context.Context) (map[domain.Status]int, error) {
			return accounts.CountByStatus(ctx, d.Now(), time.Time{})
		})
	g.Gauge("account.accounts.phone_verified", "Accounts with a verified phone.", accounts.CountWithPhone)
}
