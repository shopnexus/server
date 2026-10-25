// Package staff signs in moderators and admins, checks their session and
// role on every request, and serves the console's API under /v1/staff.
package staff

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/staff/handler"
	"github.com/shopnexus/server2/internal/staff/repository"
	"github.com/shopnexus/server2/internal/staff/service"
)

// NewService builds Staff on db. Mount, cmd/staff and the e2e tests build it
// the same way; cmd/staff passes nil accounts and auth.
func NewService(db *pgxpool.Pool, k service.Keys, accounts accountsvc.Service, auth authsvc.Service, now func() time.Time) *service.Staff {
	return service.New(service.Deps{
		Members: repository.NewMembers(db, k.Secrets), Locks: repository.NewLocks(db),
		Sessions: repository.NewSessions(db), Actions: audit.NewPGStore(db), Keys: k,
		Accounts: accounts, Auth: auth, Now: now,
	})
}

// Mount registers the staff endpoints. accounts and auth reach the customer
// data of modules account and auth.
func Mount(d module.Deps, k service.Keys, accounts accountsvc.Service, auth authsvc.Service) {
	handler.New(NewService(d.Pool, k, accounts, auth, d.Now), d.Log).Register(d.Router)
}
