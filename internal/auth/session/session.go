// Package session holds signed-in devices: the session with its rotating
// refresh token, the access tokens signed for it, and the store.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

const (
	AccessTTL = 15 * time.Minute
	MaxAge    = 60 * 24 * time.Hour
	IdleLimit = 30 * 24 * time.Hour
)

var (
	ErrNotFound       = errors.New("session: not found")
	ErrConflict       = errors.New("session: changed meanwhile")
	ErrInvalidRefresh = apperr.New(apperr.KindUnauthenticated, "invalid_refresh_token", "the refresh token is not valid")
	ErrRevoked        = apperr.New(apperr.KindUnauthenticated, "session_revoked", "the session was signed out")
	ErrExpired        = apperr.New(apperr.KindUnauthenticated, "session_expired", "the session has expired; sign in again")
	ErrInvalidToken   = apperr.New(apperr.KindUnauthenticated, "invalid_token", "the access token is not valid")
)

// Session is one signed-in device.
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
	writeID         uuid.UUID
}

// New starts a session and returns it with its refresh token; only the
// token's hash is kept. The first write ID is the session ID.
func New(id, accountID uuid.UUID, deviceName string, authTime, now time.Time) (Session, string) {
	token, hash := NewRefreshToken()
	return Session{
		id: id, accountID: accountID, refreshHash: hash, deviceName: deviceName, authTime: authTime,
		createdAt: now, lastUsedAt: now, expiresAt: now.Add(MaxAge), writeID: id,
	}, token
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

// Usable returns nil while the session may refresh: not revoked, younger than
// MaxAge, and used within IdleLimit.
func (s *Session) Usable(now time.Time) error {
	if s.revokedAt != nil {
		return ErrRevoked
	}
	if !now.Before(s.expiresAt) || !now.Before(s.lastUsedAt.Add(IdleLimit)) {
		return ErrExpired
	}
	return nil
}

// Rotate replaces the refresh token, keeps the old hash to recognise a
// replay, and returns the new token.
func (s *Session) Rotate(now time.Time) string {
	token, hash := NewRefreshToken()
	s.prevRefreshHash, s.refreshHash, s.lastUsedAt = s.refreshHash, hash, now
	return token
}

// Revoke signs the session out; a second call keeps the first time.
func (s *Session) Revoke(now time.Time) {
	if s.revokedAt == nil {
		s.revokedAt = &now
	}
}

// NewRefreshToken returns rt_ plus 32 random bytes in base64url, and its hash.
func NewRefreshToken() (string, []byte) {
	b := make([]byte, 32)
	rand.Read(b)
	token := "rt_" + base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefresh(token)
}

// HashRefresh is plain SHA-256: the token carries 256 random bits, so a slow
// hash would add nothing.
func HashRefresh(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
