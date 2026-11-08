package domain

import (
	"net/netip"
	"time"
	"uuid"
)

// SessionTTL is how long a console session lasts; nothing extends it.
const SessionTTL = 8 * time.Hour

// Session is one sign-in in one browser. Its cookie holds a random token and
// the row holds the token's hash. A session lives while it is unrevoked,
// younger than SessionTTL, and its member is active at the credentials
// version the session was issued at; a password change or reset, a TOTP
// reset and a disable raise the version, which ends every session of the
// member.
type Session struct {
	ID                 uuid.UUID
	MemberID           uuid.UUID
	TokenHash          []byte
	CredentialsVersion int
	IP                 netip.Addr
	UserAgent          string
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

// Grant is a live session as the console handler's Require reads it.
type Grant struct {
	SessionID uuid.UUID
	MemberID  uuid.UUID
	Role      Role
}
