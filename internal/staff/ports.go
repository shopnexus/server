package staff

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// AccountPort is what staff needs from module account; account.Client
// implements it. Suspend and Reinstate record their audit row in the
// account's own transaction.
type AccountPort interface {
	Record(ctx context.Context, id uuid.UUID) (account.Record, error)
	Records(ctx context.Context, p page.Request) ([]account.Record, error)
	Suspend(ctx context.Context, id uuid.UUID, reason string, until *time.Time, by audit.Actor) (account.Record, error)
	Reinstate(ctx context.Context, id uuid.UUID, by audit.Actor) (account.Record, error)
}

// IdentityPort is what staff needs from module auth; auth.Client implements
// it.
type IdentityPort interface {
	FindAccount(ctx context.Context, q string) (uuid.UUID, bool, error)
	Identities(ctx context.Context, accountID uuid.UUID) ([]auth.Identity, error)
}
