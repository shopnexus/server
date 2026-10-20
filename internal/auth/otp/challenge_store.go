package otp

import (
	"context"
	"uuid"
)

// ChallengeStore persists challenges. Each method is its own transaction.
type ChallengeStore interface {
	// Create inserts c; the same id again does nothing.
	Create(ctx context.Context, c Challenge) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (Challenge, error)
	// Save writes attempts and consumed_at when the row still has c.WriteID()
	// or already has writeID, then sets c's write ID. Otherwise ErrConflict.
	Save(ctx context.Context, c *Challenge, writeID uuid.UUID) error
	// IsNewest reports whether no later challenge exists for c's destination
	// and purpose.
	IsNewest(ctx context.Context, c Challenge) (bool, error)
}
