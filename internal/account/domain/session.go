package domain

import (
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/token"
)

const (
	AccessTTL        = 15 * time.Minute
	SessionMaxAge    = 60 * 24 * time.Hour
	SessionIdleLimit = 30 * 24 * time.Hour
)

// RevokeReason is why a session ended. Sessions revoked before 2026-11-17
// have none.
type RevokeReason string

const (
	// RevokedSignOut is a sign-out: of this device, of one device from the
	// device list, or of every other device.
	RevokedSignOut RevokeReason = "sign_out"
	// RevokedReuse is a refresh token two parties presented.
	RevokedReuse RevokeReason = "reuse"
	// RevokedSuspended is a refresh that found the account suspended.
	RevokedSuspended RevokeReason = "suspended"
	// RevokedDeleted is a session of a deleted account.
	RevokedDeleted RevokeReason = "deleted"
)

// Session is one signed-in device, with its rotating refresh token.
type Session struct {
	id              uuid.UUID
	accountID       uuid.UUID
	refreshHash     []byte
	prevRefreshHash []byte
	deviceName      string
	authTime        time.Time
	createdAt       time.Time
	lastUsedAt      time.Time
	expiresAt       time.Time
	revokedAt       *time.Time
	revokeReason    RevokeReason
	writeID         uuid.UUID
}

// NewSession starts a session and returns it with its refresh token; only the
// token's hash is kept. The first write ID is the session ID.
func NewSession(id, accountID uuid.UUID, deviceName string, authTime, now time.Time) (s Session, refreshToken string) {
	refresh, hash := NewRefreshToken()
	return Session{
		id: id, accountID: accountID, refreshHash: hash, deviceName: deviceName, authTime: authTime,
		createdAt: now, lastUsedAt: now, expiresAt: now.Add(SessionMaxAge), writeID: id,
	}, refresh
}

func (s *Session) ID() uuid.UUID           { return s.id }
func (s *Session) AccountID() uuid.UUID    { return s.accountID }
func (s *Session) RefreshHash() []byte     { return s.refreshHash }
func (s *Session) PrevRefreshHash() []byte { return s.prevRefreshHash }
func (s *Session) DeviceName() string      { return s.deviceName }
func (s *Session) AuthTime() time.Time     { return s.authTime }
func (s *Session) CreatedAt() time.Time    { return s.createdAt }
func (s *Session) LastUsedAt() time.Time   { return s.lastUsedAt }
func (s *Session) ExpiresAt() time.Time    { return s.expiresAt }
func (s *Session) RevokedAt() *time.Time   { return s.revokedAt }
func (s *Session) WriteID() uuid.UUID      { return s.writeID }

// RevokeReason is why the session ended; empty while it lives and for a
// session revoked before reasons were kept.
func (s *Session) RevokeReason() RevokeReason { return s.revokeReason }

// RestoreSession rebuilds a stored session. Stores only.
func RestoreSession(id, accountID uuid.UUID, refreshHash, prevRefreshHash []byte, deviceName string,
	authTime, createdAt, lastUsedAt, expiresAt time.Time, revokedAt *time.Time, revokeReason RevokeReason, writeID uuid.UUID,
) Session {
	return Session{
		id: id, accountID: accountID, refreshHash: refreshHash, prevRefreshHash: prevRefreshHash, deviceName: deviceName,
		authTime: authTime, createdAt: createdAt, lastUsedAt: lastUsedAt, expiresAt: expiresAt, revokedAt: revokedAt,
		revokeReason: revokeReason, writeID: writeID,
	}
}

// Saved records that a store wrote s under writeID. Stores only.
func (s *Session) Saved(writeID uuid.UUID) { s.writeID = writeID }

// Usable returns nil while the session may refresh: not revoked, younger than
// SessionMaxAge, and used within SessionIdleLimit.
func (s *Session) Usable(now time.Time) error {
	if s.revokedAt != nil {
		return ErrSessionRevoked
	}
	if !now.Before(s.expiresAt) || !now.Before(s.lastUsedAt.Add(SessionIdleLimit)) {
		return ErrSessionExpired
	}
	return nil
}

// Rotate replaces the refresh token, keeps the old hash to recognise a
// replay, and returns the new token.
func (s *Session) Rotate(now time.Time) string {
	refresh, hash := NewRefreshToken()
	s.prevRefreshHash, s.refreshHash, s.lastUsedAt = s.refreshHash, hash, now
	return refresh
}

// Revoke ends the session for reason; a second call keeps the first time
// and reason.
func (s *Session) Revoke(reason RevokeReason, now time.Time) {
	if s.revokedAt == nil {
		s.revokedAt, s.revokeReason = &now, reason
	}
}

// NewRefreshToken returns a fresh refresh token and its hash.
func NewRefreshToken() (refresh string, hash []byte) { return token.NewOpaque("rt_") }

// HashRefresh hashes a refresh token for lookup.
func HashRefresh(t string) []byte { return token.Hash(t) }
