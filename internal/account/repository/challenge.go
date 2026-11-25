package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/contact"
)

// Challenges persists email code challenges.
type Challenges interface {
	// Create inserts c unless a challenge of key goes to c's destination,
	// and returns the ID stored: c.ID() or the earlier one. The same id again
	// does nothing; uuid.Nil() claims no key.
	Create(ctx context.Context, c domain.Challenge, key uuid.UUID) (uuid.UUID, error)
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Challenge, error)
	// ByKey returns the challenge of key to dest, or ErrNotFound.
	ByKey(ctx context.Context, dest contact.Address, key uuid.UUID) (domain.Challenge, error)
	// Save writes attempts and consumed_at when the row still has c.WriteID()
	// or already has writeID, then sets c's write ID. Otherwise ErrConflict.
	Save(ctx context.Context, c *domain.Challenge, writeID uuid.UUID) error
	// IsNewest reports whether no later challenge exists for c's destination
	// and purpose.
	IsNewest(ctx context.Context, c domain.Challenge) (bool, error)
}
