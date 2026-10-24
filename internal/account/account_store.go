package account

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Store persists accounts. Each method is its own transaction.
type Store interface {
	// Create inserts a; inserting the same id again does nothing, so a retried
	// step is safe.
	Create(ctx context.Context, a Account) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (Account, error)
	// Save writes a when its row still has a.WriteID() or already has
	// writeID, then sets a's write ID to writeID. Otherwise ErrConflict.
	Save(ctx context.Context, a *Account, writeID uuid.UUID) error
	// SaveAudited is Save that writes e in the same transaction; a conflict
	// writes neither.
	SaveAudited(ctx context.Context, a *Account, writeID uuid.UUID, e audit.Entry) error
	// List is a query: accounts with IDs below p.Before(), newest first, at
	// most p.Fetch().
	List(ctx context.Context, p page.Request) ([]Account, error)
	// Delete removes the account; deleting a missing one does nothing. Only
	// the sign-up compensation calls it.
	Delete(ctx context.Context, id uuid.UUID) error
}
