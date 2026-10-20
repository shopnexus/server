package otp

import (
	"context"
	"uuid"
)

// QuotaStore persists quotas. Each method is its own transaction.
type QuotaStore interface {
	// Get returns the quota for key, or an empty one when none is stored.
	Get(ctx context.Context, key string) (Quota, error)
	// Save writes q when its row still has q.WriteID() (no row, for a quota
	// never saved) or already has writeID. Otherwise ErrConflict.
	Save(ctx context.Context, q *Quota, writeID uuid.UUID) error
}
