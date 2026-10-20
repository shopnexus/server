package account

import (
	"context"
	"uuid"
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
	// Delete removes the account; deleting a missing one does nothing. Only
	// the sign-up compensation calls it.
	Delete(ctx context.Context, id uuid.UUID) error
}
