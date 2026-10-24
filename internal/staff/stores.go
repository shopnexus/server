package staff

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/member"
	"github.com/shopnexus/server2/internal/staff/quota"
	"github.com/shopnexus/server2/internal/staff/session"
)

// Stores is every adapter staff uses. Mount, cmd/staff and the e2e tests
// build them the same way.
type Stores struct {
	Members  member.Store
	Quotas   quota.Store
	Sessions session.Store
	Actions  audit.Store
}

func NewStores(db *pgxpool.Pool, k Keys) Stores {
	return Stores{
		Members: member.NewPGStore(db, k.Secrets), Quotas: quota.NewPGStore(db),
		Sessions: session.NewPGStore(db), Actions: audit.NewPGStore(db),
	}
}
