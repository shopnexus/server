package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
)

// Quotas persists OTP send quotas.
type Quotas interface {
	// Get returns the quota for key, or an empty one when none is stored.
	Get(ctx context.Context, key string) (domain.Quota, error)
	// Save writes q when its row still has q.WriteID() (no row, for a quota
	// never saved) or already has writeID. Otherwise ErrConflict.
	Save(ctx context.Context, q *domain.Quota, writeID uuid.UUID) error
}
