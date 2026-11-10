// Package console signs in moderators and admins, checks their session and
// role on every request, and serves the console's API under /v1/console.
package console

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/console/handler"
	"github.com/shopnexus/server2/internal/console/repository"
	"github.com/shopnexus/server2/internal/console/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/module"
	supportsvc "github.com/shopnexus/server2/internal/support/service"
)

// Settings is what console's Mount takes beyond module.Deps.
type Settings struct {
	// Keys are derived from Config.Key.
	Keys   service.Keys
	Config config.ConsoleConfig
}

// NewService builds Console on db. Mount, cmd/console and the e2e tests build
// it the same way; cmd/console passes nil accounts.
func NewService(db *pgxpool.Pool, s Settings, accounts accountsvc.Service, now func() time.Time) *service.Console {
	return service.New(service.Deps{
		Members: repository.NewMembers(db, s.Keys.Secrets), Locks: repository.NewLocks(db),
		Sessions: repository.NewSessions(db), Actions: audit.NewPGStore(db), Keys: s.Keys,
		Dashboards: repository.NewDashboards(db), DashboardHosts: s.Config.DashboardHosts, ConsoleOrigin: s.Config.Origin,
		Accounts: accounts, Now: now,
	})
}

// Mount registers the console endpoints and the ops dashboards' callback.
// accounts reaches the customer data of module account, and feedback the
// feedback module support keeps.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, feedback supportsvc.Service) {
	handler.New(NewService(d.Pool, s, accounts, d.Now), feedback, d.Log).Register(d.Router)
}
