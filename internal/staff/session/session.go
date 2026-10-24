// Package session holds staff sessions: one sign-in in one browser, eight
// hours long, with a refresh token that rotates in an HttpOnly cookie. A
// session lives only while its member's credentials version equals the one
// it was issued at.
package session

import (
	"errors"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/member"
)

const (
	TTL          = 8 * time.Hour
	AccessTTL    = 5 * time.Minute
	maxUserAgent = 500
)

var (
	ErrNotFound       = errors.New("staff session: not found")
	ErrConflict       = errors.New("staff session: changed meanwhile")
	ErrInvalidRefresh = apperr.New(apperr.KindUnauthenticated, "invalid_refresh_token", "the refresh token is not valid")
	ErrRevoked        = apperr.New(apperr.KindUnauthenticated, "session_revoked", "the session was signed out")
	ErrExpired        = apperr.New(apperr.KindUnauthenticated, "session_expired", "the session has expired; sign in again")
)

type Session struct {
	id                 uuid.UUID
	staffID            uuid.UUID
	refreshHash        []byte
	prevRefreshHash    []byte
	credentialsVersion int
	ip                 netip.Addr
	userAgent          string
	createdAt          time.Time
	lastUsedAt         time.Time
	expiresAt          time.Time
	revokedAt          *time.Time
	writeID            uuid.UUID
}

// New starts a session at the member's credentials version and returns its
// refresh token; only the hash is kept. The first write ID is the id.
func New(id, staffID uuid.UUID, version int, ip netip.Addr, userAgent string, now time.Time) (s Session, refresh string) {
	refresh, hash := token.NewRefresh()
	if r := []rune(userAgent); len(r) > maxUserAgent {
		userAgent = string(r[:maxUserAgent])
	}
	return Session{
		id: id, staffID: staffID, refreshHash: hash, prevRefreshHash: nil, credentialsVersion: version, ip: ip,
		userAgent: userAgent, createdAt: now, lastUsedAt: now, expiresAt: now.Add(TTL), revokedAt: nil, writeID: id,
	}, refresh
}

func (s *Session) ID() uuid.UUID           { return s.id }
func (s *Session) StaffID() uuid.UUID      { return s.staffID }
func (s *Session) CredentialsVersion() int { return s.credentialsVersion }
func (s *Session) IP() netip.Addr          { return s.ip }
func (s *Session) UserAgent() string       { return s.userAgent }
func (s *Session) CreatedAt() time.Time    { return s.createdAt }
func (s *Session) ExpiresAt() time.Time    { return s.expiresAt }
func (s *Session) RevokedAt() *time.Time   { return s.revokedAt }
func (s *Session) PrevRefreshHash() []byte { return s.prevRefreshHash }

// Usable returns nil while the session is unrevoked and younger than TTL.
func (s *Session) Usable(now time.Time) error {
	if s.revokedAt != nil {
		return ErrRevoked
	}
	if !now.Before(s.expiresAt) {
		return ErrExpired
	}
	return nil
}

// Rotate replaces the refresh token, keeps the old hash to recognise a
// replay, and returns the new token.
func (s *Session) Rotate(now time.Time) string {
	refresh, hash := token.NewRefresh()
	s.prevRefreshHash, s.refreshHash, s.lastUsedAt = s.refreshHash, hash, now
	return refresh
}

// Revoke signs the session out; a second call keeps the first time.
func (s *Session) Revoke(now time.Time) {
	if s.revokedAt == nil {
		s.revokedAt = &now
	}
}

// Grant is what staff.Require reads about a session and its member.
type Grant struct {
	Role      member.Role
	Active    bool // the member is active
	Current   bool // the session's credentials version is the member's
	RevokedAt *time.Time
	ExpiresAt time.Time
}

// Check returns nil while the grant lets a request through at now.
func (g Grant) Check(now time.Time) error {
	switch {
	case g.RevokedAt != nil || !g.Active || !g.Current:
		return ErrRevoked
	case !now.Before(g.ExpiresAt):
		return ErrExpired
	default:
		return nil
	}
}
