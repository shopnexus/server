package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/console/domain"
)

// Sessions keeps console sessions. Each method is one statement.
type Sessions interface {
	Create(ctx context.Context, s domain.Session) error
	// Live is a query joining the session whose token hashes to tokenHash
	// with its member. It returns ErrNotFound unless the session lives at
	// now.
	Live(ctx context.Context, tokenHash []byte, now time.Time) (domain.Grant, error)
	// LastSignIns is a query: the start of each member's newest session, for
	// the members of ids that have one.
	LastSignIns(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]time.Time, error)
	// Revoke signs one session out; a second call keeps the first time.
	Revoke(ctx context.Context, id uuid.UUID, now time.Time) error
}
