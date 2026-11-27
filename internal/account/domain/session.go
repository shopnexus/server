package domain

import (
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/token"
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
	// RevokedSuspended is an older session a refresh revoked because the
	// account was suspended; a suspension no longer ends a session.
	RevokedSuspended RevokeReason = "suspended"
	// RevokedDeleted is a session of a deleted account.
	RevokedDeleted RevokeReason = "deleted"
)

// Session is one signed-in device, with its rotating refresh token.
type Session struct{ st SessionState }

// SessionState is every stored field of a session. Stores only.
type SessionState struct {
	ID              uuid.UUID
	AccountID       uuid.UUID
	RefreshHash     []byte
	PrevRefreshHash []byte
	DeviceName      string
	AuthTime        time.Time
	CreatedAt       time.Time
	LastUsedAt      time.Time
	ExpiresAt       time.Time
	RevokedAt       *time.Time
	RevokeReason    RevokeReason
	WriteID         uuid.UUID
}

// NewSession starts a session and returns it with its refresh token; only the
// token's hash is kept. The first write ID is the session ID.
func NewSession(id, accountID uuid.UUID, deviceName string, authTime, now time.Time) (s Session, refreshToken string) {
	refresh, hash := NewRefreshToken()
	return Session{st: SessionState{
		ID: id, AccountID: accountID, RefreshHash: hash, DeviceName: deviceName, AuthTime: authTime,
		CreatedAt: now, LastUsedAt: now, ExpiresAt: now.Add(SessionMaxAge), WriteID: id,
	}}, refresh
}

func (s *Session) ID() uuid.UUID           { return s.st.ID }
func (s *Session) AccountID() uuid.UUID    { return s.st.AccountID }
func (s *Session) RefreshHash() []byte     { return s.st.RefreshHash }
func (s *Session) PrevRefreshHash() []byte { return s.st.PrevRefreshHash }
func (s *Session) DeviceName() string      { return s.st.DeviceName }
func (s *Session) AuthTime() time.Time     { return s.st.AuthTime }
func (s *Session) CreatedAt() time.Time    { return s.st.CreatedAt }
func (s *Session) LastUsedAt() time.Time   { return s.st.LastUsedAt }
func (s *Session) ExpiresAt() time.Time    { return s.st.ExpiresAt }
func (s *Session) RevokedAt() *time.Time   { return s.st.RevokedAt }
func (s *Session) WriteID() uuid.UUID      { return s.st.WriteID }

// RevokeReason is why the session ended; empty while it lives and for a
// session revoked before reasons were kept.
func (s *Session) RevokeReason() RevokeReason { return s.st.RevokeReason }

// RestoreSession rebuilds a stored session. Stores only.
func RestoreSession(st SessionState) Session { return Session{st: st} }

// Saved is for stores only. It records that a store wrote s under writeID,
// and the auth_time its row holds, which a reauthentication moves without a
// new write ID and which may be later than the one s was read with.
func (s *Session) Saved(writeID uuid.UUID, authTime time.Time) {
	s.st.WriteID, s.st.AuthTime = writeID, authTime
}

// Usable returns nil while the session may refresh: not revoked, younger than
// SessionMaxAge, and used within SessionIdleLimit.
func (s *Session) Usable(now time.Time) error {
	if s.st.RevokedAt != nil {
		return ErrSessionRevoked
	}
	if !now.Before(s.st.ExpiresAt) || !now.Before(s.st.LastUsedAt.Add(SessionIdleLimit)) {
		return ErrSessionExpired
	}
	return nil
}

// Rotate replaces the refresh token, keeps the old hash to recognise a
// replay, and returns the new token.
func (s *Session) Rotate(now time.Time) string {
	refresh, hash := NewRefreshToken()
	s.st.PrevRefreshHash, s.st.RefreshHash, s.st.LastUsedAt = s.st.RefreshHash, hash, now
	return refresh
}

// Revoke ends the session for reason; a second call keeps the first time
// and reason.
func (s *Session) Revoke(reason RevokeReason, now time.Time) {
	if s.st.RevokedAt == nil {
		s.st.RevokedAt, s.st.RevokeReason = &now, reason
	}
}

// NewRefreshToken returns a fresh refresh token and its hash.
func NewRefreshToken() (refresh string, hash []byte) { return token.NewOpaque("rt_") }

// HashRefresh hashes a refresh token for lookup.
func HashRefresh(t string) []byte { return token.Hash(t) }
