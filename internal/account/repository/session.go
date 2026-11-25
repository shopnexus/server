package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// ActiveSession is one row of the device list.
type ActiveSession struct {
	ID         uuid.UUID
	DeviceName string
	CreatedAt  time.Time
	LastUsedAt time.Time
}

// Sessions persists sessions.
type Sessions interface {
	// Create inserts s unless the account has a session of key, and returns
	// the ID of the session stored: s.ID() or the earlier one. uuid.Nil()
	// claims no key.
	Create(ctx context.Context, s domain.Session, key uuid.UUID) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Session, error)
	FindByRefresh(ctx context.Context, hash []byte) (domain.Session, error)
	// FindByPrevRefresh finds the session a rotated token belonged to.
	FindByPrevRefresh(ctx context.Context, hash []byte) (domain.Session, error)
	// Save writes s when its row still has s.WriteID() or already has
	// writeID, then sets s's write ID and the auth_time the row holds.
	// Otherwise ErrConflict.
	Save(ctx context.Context, s *domain.Session, writeID uuid.UUID) error
	// Reauthenticate moves the auth_time of session id forward to at, in one
	// statement, and returns the session. ErrNotFound when the session is
	// unknown, revoked, or past domain.SessionMaxAge or
	// domain.SessionIdleLimit at the time given. It leaves the write ID as it was: Save never
	// writes auth_time, so the two cannot overwrite each other, and a
	// refresh racing it would otherwise read the conflict as a reused
	// token.
	Reauthenticate(ctx context.Context, id uuid.UUID, at time.Time) (domain.Session, error)
	// RevokeOthers signs out every open session of the account but keep in
	// one statement and gives each writeID, so a refresh racing it
	// conflicts.
	RevokeOthers(ctx context.Context, accountID, keep uuid.UUID, now time.Time, writeID uuid.UUID) error
	// ActiveAmong returns which of ids are usable at now: neither revoked,
	// expired nor idle past domain.SessionIdleLimit.
	ActiveAmong(ctx context.Context, ids []uuid.UUID, now time.Time) (map[uuid.UUID]bool, error)
	// ListActive is a query: the account's usable sessions at now, newest
	// first, below p.Before(), at most p.Fetch().
	ListActive(ctx context.Context, accountID uuid.UUID, now time.Time, p page.Request) ([]ActiveSession, error)
}
