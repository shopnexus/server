package domain

import (
	"crypto/hmac"
	"encoding/json"
	"net/netip"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/contact"
)

// Purpose says what a code is for: signing in (and signing up), or adding
// an email to the account that asked.
type Purpose string

const (
	PurposeSignIn      Purpose = "sign_in"
	PurposeAddIdentity Purpose = "add_identity"
)

const (
	CodeTTL     = 5 * time.Minute
	MaxAttempts = 5
)

// Challenge is one code sent to one destination.
type Challenge struct{ s ChallengeState }

// ChallengeState is every stored field of a challenge, and how a Restate
// step journals one it loaded. Stores only.
type ChallengeState struct {
	ID         uuid.UUID       `json:"id"`
	Purpose    Purpose         `json:"purpose"`
	Dest       contact.Address `json:"dest"`
	AccountID  *uuid.UUID      `json:"account_id,omitempty"`
	CodeHash   []byte          `json:"code_hash"`
	Attempts   int             `json:"attempts"`
	IP         netip.Addr      `json:"ip"`
	ExpiresAt  time.Time       `json:"expires_at"`
	CreatedAt  time.Time       `json:"created_at"`
	ConsumedAt *time.Time      `json:"consumed_at,omitempty"`
	WriteID    uuid.UUID       `json:"write_id"`
}

// NewChallenge starts a challenge that expires CodeTTL after now. An
// add_identity challenge names the account that asked for it, a sign_in one
// nil. Its first write ID is its id.
func NewChallenge(id uuid.UUID, purpose Purpose, dest contact.Address, accountID *uuid.UUID, ip netip.Addr, codeHash []byte, now time.Time) Challenge {
	return Challenge{s: ChallengeState{
		ID: id, Purpose: purpose, Dest: dest, AccountID: accountID, CodeHash: codeHash, IP: ip,
		ExpiresAt: now.Add(CodeTTL), CreatedAt: now, WriteID: id,
	}}
}

// RestoreChallenge rebuilds a stored challenge. Stores only.
func RestoreChallenge(st ChallengeState) Challenge { return Challenge{s: st} }

func (c Challenge) MarshalJSON() ([]byte, error) { return json.Marshal(c.s) }

func (c *Challenge) UnmarshalJSON(b []byte) error {
	var st ChallengeState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	*c = Challenge{s: st}
	return nil
}

func (c *Challenge) ID() uuid.UUID                { return c.s.ID }
func (c *Challenge) Purpose() Purpose             { return c.s.Purpose }
func (c *Challenge) Destination() contact.Address { return c.s.Dest }
func (c *Challenge) AccountID() *uuid.UUID        { return c.s.AccountID }
func (c *Challenge) CodeHash() []byte             { return c.s.CodeHash }
func (c *Challenge) Attempts() int                { return c.s.Attempts }
func (c *Challenge) IP() netip.Addr               { return c.s.IP }
func (c *Challenge) ExpiresAt() time.Time         { return c.s.ExpiresAt }
func (c *Challenge) CreatedAt() time.Time         { return c.s.CreatedAt }
func (c *Challenge) ConsumedAt() *time.Time       { return c.s.ConsumedAt }
func (c *Challenge) WriteID() uuid.UUID           { return c.s.WriteID }

// Saved records that a store wrote c under writeID. Stores only.
func (c *Challenge) Saved(writeID uuid.UUID) { c.s.WriteID = writeID }

// Open returns nil while c accepts a guess at now: not used, not expired and
// not locked.
func (c *Challenge) Open(now time.Time) error {
	switch {
	case c.s.ConsumedAt != nil, !now.Before(c.s.ExpiresAt):
		return ErrCodeExpired
	case c.s.Attempts >= MaxAttempts:
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
	if hmac.Equal(codeHash, c.s.CodeHash) {
		c.s.ConsumedAt = &now
	} else {
		c.s.Attempts++
	}
	return c.verdict()
}

// Repeat repeats what the last counted guess decided, for a retry of the
// request that made it: nil only when that guess consumed c with codeHash.
func (c *Challenge) Repeat(codeHash []byte) error {
	if c.s.ConsumedAt != nil && !hmac.Equal(codeHash, c.s.CodeHash) {
		return ErrCodeExpired
	}
	return c.verdict()
}

// verdict is what the last counted guess decided: nil when it consumed c.
func (c *Challenge) verdict() error {
	switch {
	case c.s.ConsumedAt != nil:
		return nil
	case c.s.Attempts >= MaxAttempts:
		return ErrCodeLocked
	}
	return ErrInvalidCode.WithAttemptsLeft(MaxAttempts - c.s.Attempts)
}
