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
)

// NewService builds Console on db with the keys derived from cfg. Mount,
// cmd/console and the e2e tests build it the same way; cmd/console passes nil
// accounts.
func NewService(db *pgxpool.Pool, k service.Keys, cfg config.ConsoleConfig, accounts accountsvc.Service, now func() time.Time) *service.Console {
	return service.New(service.Deps{
		Members: repository.NewMembers(db, k.Secrets), Locks: repository.NewLocks(db),
		Sessions: repository.NewSessions(db), Actions: audit.NewPGStore(db), Keys: k,
		Dashboards: repository.NewDashboards(db), DashboardHosts: cfg.DashboardHosts, ConsoleOrigin: cfg.Origin,
		Accounts: accounts, Now: now,
	})
}

// Mount registers the console endpoints and the ops dashboards' callback.
// accounts reaches the customer data of module account.
func Mount(d module.Deps, k service.Keys, cfg config.ConsoleConfig, accounts accountsvc.Service) {
	handler.New(NewService(d.Pool, k, cfg, accounts, d.Now), d.Log).Register(d.Router)
}
