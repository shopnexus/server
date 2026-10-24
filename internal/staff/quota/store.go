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
	// Charge admits one sign-in attempt on key at now and counts it as a
	// failure until Refund or Clear takes it back, in one statement, so
	// concurrent attempts cannot pass a check together. While the email is
	// unlocked it admits at most MaxFailures charged attempts; it refuses the
	// rest, and every attempt while the email is locked.
	Charge(ctx context.Context, key []byte, now time.Time) (Admission, error)
	// Refund takes back the charge of an attempt whose password was right.
	Refund(ctx context.Context, key []byte, now time.Time) error
	// Fail records a charged attempt that failed as e. The attempt charged
	// as the MaxFailures-th locks the email for LockFor in the same
	// transaction.
	Fail(ctx context.Context, key []byte, charged int, now time.Time, e audit.Entry) error
	// Clear forgets every failure and any lock, after a finished sign-in.
	Clear(ctx context.Context, key []byte, now time.Time) error
	// LockedUntil is a query: for each of keys locked at now, the end of the
	// lock, keyed by string(key).
	LockedUntil(ctx context.Context, keys [][]byte, now time.Time) (map[string]time.Time, error)
}
