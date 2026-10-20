// Package otp holds one-time code challenges, the derivation of their codes,
// and the send quotas. It sends nothing; the auth use cases do.
package otp

import (
	"crypto/hmac"
	"errors"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
)

type Purpose string

// PurposeLogin signs a person in and creates the account on first use. Plan 3
// adds add_identity and reset_password.
const PurposeLogin Purpose = "login"

const (
	CodeTTL     = 5 * time.Minute
	MaxAttempts = 5
)

var (
	ErrNotFound    = errors.New("otp: not found")
	ErrConflict    = errors.New("otp: changed meanwhile")
	ErrInvalidCode = apperr.New(apperr.KindInvalid, "invalid_code", "the code does not match")
	ErrCodeExpired = apperr.New(apperr.KindInvalid, "code_expired", "the code has expired or was replaced; request a new one")
	ErrCodeLocked  = apperr.New(apperr.KindInvalid, "code_locked", "too many wrong codes; request a new one")
)

// Challenge is one code sent to one destination.
type Challenge struct {
	id         uuid.UUID
	purpose    Purpose
	dest       contact.Address
	accountID  *uuid.UUID
	codeHash   []byte
	attempts   int
	ip         netip.Addr
	expiresAt  time.Time
	createdAt  time.Time
	consumedAt *time.Time
	writeID    uuid.UUID
}

// New starts a challenge that expires CodeTTL after now. Its first write ID is
// its id.
func New(id uuid.UUID, purpose Purpose, dest contact.Address, ip netip.Addr, codeHash []byte, now time.Time) Challenge {
	return Challenge{id: id, purpose: purpose, dest: dest, ip: ip, codeHash: codeHash,
		createdAt: now, expiresAt: now.Add(CodeTTL), writeID: id}
}

// Restore rebuilds a stored challenge. Stores only.
func Restore(id uuid.UUID, purpose Purpose, dest contact.Address, accountID *uuid.UUID, codeHash []byte, attempts int,
	ip netip.Addr, expiresAt, createdAt time.Time, consumedAt *time.Time, writeID uuid.UUID) Challenge {
	return Challenge{id: id, purpose: purpose, dest: dest, accountID: accountID, codeHash: codeHash, attempts: attempts,
		ip: ip, expiresAt: expiresAt, createdAt: createdAt, consumedAt: consumedAt, writeID: writeID}
}

func (c Challenge) ID() uuid.UUID                { return c.id }
func (c Challenge) Purpose() Purpose             { return c.purpose }
func (c Challenge) Destination() contact.Address { return c.dest }
func (c Challenge) AccountID() *uuid.UUID        { return c.accountID }
func (c Challenge) CodeHash() []byte             { return c.codeHash }
func (c Challenge) Attempts() int                { return c.attempts }
func (c Challenge) IP() netip.Addr               { return c.ip }
func (c Challenge) ExpiresAt() time.Time         { return c.expiresAt }
func (c Challenge) CreatedAt() time.Time         { return c.createdAt }
func (c Challenge) ConsumedAt() *time.Time       { return c.consumedAt }
func (c Challenge) WriteID() uuid.UUID           { return c.writeID }

// Open returns nil while c accepts a guess at now: not used, not expired and
// not locked.
func (c Challenge) Open(now time.Time) error {
	switch {
	case c.consumedAt != nil, !now.Before(c.expiresAt):
		return ErrCodeExpired
	case c.attempts >= MaxAttempts:
		return ErrCodeLocked
	}
	return nil
}

// Verify takes a guess at now. A challenge that is not Open refuses it and
// does not change. Otherwise a wrong code counts an attempt and a right one
// consumes c, and the caller saves c either way.
func (c *Challenge) Verify(codeHash []byte, now time.Time) error {
	if err := c.Open(now); err != nil {
		return err
	}
	if hmac.Equal(codeHash, c.codeHash) {
		c.consumedAt = &now
	} else {
		c.attempts++
	}
	return c.Verdict()
}

// Verdict is what the last counted guess decided: nil when it consumed c.
func (c Challenge) Verdict() error {
	switch {
	case c.consumedAt != nil:
		return nil
	case c.attempts >= MaxAttempts:
		return ErrCodeLocked
	}
	return ErrInvalidCode.WithDetail("attempts_left", MaxAttempts-c.attempts)
}
