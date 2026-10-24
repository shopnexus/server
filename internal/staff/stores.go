package staff

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/staff/member"
)

// Stores is every adapter staff uses. Mount, cmd/staff and the e2e tests
// build them the same way.
type Stores struct {
	Members  member.Store
	Locks    LockStore
	Sessions SessionStore
	Actions  audit.Store
}

func NewStores(db *pgxpool.Pool, k Keys) Stores {
	return Stores{
		Members: member.NewPGStore(db, k.Secrets), Locks: NewPGLockStore(db),
		Sessions: NewPGSessionStore(db), Actions: audit.NewPGStore(db),
	}
}
