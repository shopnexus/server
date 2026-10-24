package quota

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
)

// Store persists quotas. Each method is its own transaction.
type Store interface {
	// Get returns New(key) when the email has no row.
	Get(ctx context.Context, key []byte) (Quota, error)
	// Save writes q when its row still has q's write ID, or already has
	// writeID; a quota never saved conflicts with any row. Otherwise
	// ErrConflict.
	Save(ctx context.Context, q *Quota, writeID uuid.UUID) error
	// SaveAudited is Save that writes e in the same transaction.
	SaveAudited(ctx context.Context, q *Quota, writeID uuid.UUID, e audit.Entry) error
	// LockedUntil is a query: for each of keys locked at now, the end of the
	// lock, keyed by string(key).
	LockedUntil(ctx context.Context, keys [][]byte, now time.Time) (map[string]time.Time, error)
}
