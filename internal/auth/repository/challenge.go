package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
)

// Challenges persists OTP challenges.
type Challenges interface {
	// Create inserts c; the same id again does nothing.
	Create(ctx context.Context, c domain.Challenge) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Challenge, error)
	// Save writes attempts and consumed_at when the row still has c.WriteID()
	// or already has writeID, then sets c's write ID. Otherwise ErrConflict.
	Save(ctx context.Context, c *domain.Challenge, writeID uuid.UUID) error
	// IsNewest reports whether no later challenge exists for c's destination
	// and purpose.
	IsNewest(ctx context.Context, c domain.Challenge) (bool, error)
}
