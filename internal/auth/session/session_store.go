package session

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

// View is one row of the device list.
type View struct {
	ID         uuid.UUID
	DeviceName string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// Store persists sessions. Each method is its own transaction.
type Store interface {
	// Create inserts s and reports whether it did; false means the id exists.
	Create(ctx context.Context, s Session) (bool, error)
	Get(ctx context.Context, id uuid.UUID) (Session, error)
	FindByRefresh(ctx context.Context, hash []byte) (Session, error)
	// FindByPrevRefresh finds the session a rotated token belonged to.
	FindByPrevRefresh(ctx context.Context, hash []byte) (Session, error)
	// Save writes s when its row still has s.WriteID() or already has
	// writeID, then sets s's write ID. Otherwise ErrConflict.
	Save(ctx context.Context, s *Session, writeID uuid.UUID) error
	// RevokeAll revokes every open session of the account in one statement
	// and gives each a new write ID, so a refresh racing it conflicts.
	RevokeAll(ctx context.Context, accountID uuid.UUID, now time.Time) error
	// ListActive is a query: the account's usable sessions at now, newest
	// first, below p.Before(), at most p.Fetch().
	ListActive(ctx context.Context, accountID uuid.UUID, now time.Time, p page.Request) ([]View, error)
}
