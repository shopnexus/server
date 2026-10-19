package account

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/postgres"
)

// Store persists accounts. Every method takes the Querier to run on, so a
// caller can include it in its own transaction.
type Store interface {
	// Insert stores a and sets its id and createdAt.
	Insert(ctx context.Context, q postgres.Querier, a *Account) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, q postgres.Querier, id uuid.UUID) (Account, error)
	// Update writes every field of a; ErrNotFound for an unknown id.
	Update(ctx context.Context, q postgres.Querier, a Account) error
}
