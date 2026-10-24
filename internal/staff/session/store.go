package session

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
)

// Store persists staff sessions. Each method is its own transaction.
type Store interface {
	// CreateAudited inserts s and writes e.
	CreateAudited(ctx context.Context, s Session, e audit.Entry) error
	Get(ctx context.Context, id uuid.UUID) (Session, error)
	FindByRefresh(ctx context.Context, hash []byte) (Session, error)
	FindByPrevRefresh(ctx context.Context, hash []byte) (Session, error)
	// Save writes s when its row still has s's write ID or already has
	// writeID. Otherwise ErrConflict.
	Save(ctx context.Context, s *Session, writeID uuid.UUID) error
	// SaveAudited is Save that writes e in the same transaction.
	SaveAudited(ctx context.Context, s *Session, writeID uuid.UUID, e audit.Entry) error
	// MoveVersion moves a live session from credentials version from to to,
	// which keeps it alive through its own member's password change. It is
	// ErrRevoked when the session was revoked or moved meanwhile.
	MoveVersion(ctx context.Context, id uuid.UUID, from, to int) error
	// Grant is a query joining the session with its member. ErrNotFound when
	// the session does not exist or is not staffID's.
	Grant(ctx context.Context, id, staffID uuid.UUID) (Grant, error)
}
