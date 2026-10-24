package staff

import (
	"context"
	"errors"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/staff/member"
)

// SessionTTL is how long a staff session lasts; nothing extends it.
const SessionTTL = 8 * time.Hour

var errSessionNotFound = errors.New("staff session: not found")

// Session is one sign-in in one browser. Its cookie holds a random token and
// the row holds the token's hash. A session lives while it is unrevoked,
// younger than SessionTTL, and its member is active at the credentials
// version the session was issued at; a password change or reset, a TOTP
// reset and a disable raise the version, which ends every session of the
// member.
type Session struct {
	ID                 uuid.UUID
	StaffID            uuid.UUID
	TokenHash          []byte
	CredentialsVersion int
	IP                 netip.Addr
	UserAgent          string
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

// Grant is a live session as staff.Require reads it.
type Grant struct {
	SessionID uuid.UUID
	StaffID   uuid.UUID
	Role      member.Role
}

// SessionStore keeps staff sessions. Each method is one statement.
type SessionStore interface {
	Create(ctx context.Context, s Session) error
	// Live is a query joining the session whose token hashes to tokenHash
	// with its member. It returns errSessionNotFound unless the session
	// lives at now.
	Live(ctx context.Context, tokenHash []byte, now time.Time) (Grant, error)
	// Revoke signs one session out; a second call keeps the first time.
	Revoke(ctx context.Context, id uuid.UUID, now time.Time) error
}
