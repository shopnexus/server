// Package domain holds module account's entities and their rules: the
// account, its addresses, its credentials, and sessions with the tokens
// signed for them. It sends nothing and stores nothing.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// Status is whether an account may act.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	// StatusDeleted is an account its person deleted: the row stays, without
	// a name, bio or avatar, for the orders and messages that point to it.
	StatusDeleted Status = "deleted"
)

// ShopNexusID is the system account every support chat is with. Migration
// 0044 makes it with no identity, so nobody signs in to it; members of the
// console write as it.
var ShopNexusID = uuid.MustParse("0193e000-0000-7000-8000-000000000001")

// RecentAuth is how long after a sign-in the sensitive actions stay open:
// signing out everywhere and deleting the account.
const RecentAuth = 15 * time.Minute

// MaxAway is how far ahead a person may set away mode.
const MaxAway = 30 * 24 * time.Hour

const (
	maxNameRunes = 50
	maxBioRunes  = 200
)

// Account is one person. Fields are unexported so every change goes through
// a method that keeps the rules.
type Account struct{ s AccountState }

// AccountState is every stored field of an account. Stores only.
type AccountState struct {
	ID             uuid.UUID
	DisplayName    string
	Bio            string
	AvatarKey      string // the object key of the avatar; empty without one
	Status         Status
	SuspendedUntil *time.Time
	SuspendReason  string
	WriteID        uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// LastActiveAt is when the person last had the app open; nil before the
	// first time. Only the WebSocket writes it, outside the aggregate's saves.
	LastActiveAt *time.Time
	// DeletedAt is when the person deleted the account; nil while it lives.
	DeletedAt *time.Time
	// AwayUntil ends away mode; nil when not set. A past time is not away.
	AwayUntil *time.Time
}

// New builds an active account with the caller's id. The display name may
// be empty until the person completes their profile. The first write ID is
// the id.
func New(id uuid.UUID, displayName string, now time.Time) (Account, error) {
	name := strings.TrimSpace(displayName)
	if utf8.RuneCountInString(name) > maxNameRunes {
		return Account{}, ErrInvalidAccount.WithField("display_name", apperr.TooLong, "at most 50 characters")
	}
	return Account{s: AccountState{ID: id, DisplayName: name, Status: StatusActive, WriteID: id, CreatedAt: now, UpdatedAt: now}}, nil
}

// RestoreAccount rebuilds a stored account. Stores only.
func RestoreAccount(st AccountState) Account { return Account{s: st} }

func (a *Account) ID() uuid.UUID              { return a.s.ID }
func (a *Account) DisplayName() string        { return a.s.DisplayName }
func (a *Account) Bio() string                { return a.s.Bio }
func (a *Account) AvatarKey() string          { return a.s.AvatarKey }
func (a *Account) SuspendReason() string      { return a.s.SuspendReason }
func (a *Account) SuspendedUntil() *time.Time { return a.s.SuspendedUntil }
func (a *Account) CreatedAt() time.Time       { return a.s.CreatedAt }
func (a *Account) UpdatedAt() time.Time       { return a.s.UpdatedAt }
func (a *Account) LastActiveAt() *time.Time   { return a.s.LastActiveAt }
func (a *Account) WriteID() uuid.UUID         { return a.s.WriteID }
func (a *Account) DeletedAt() *time.Time      { return a.s.DeletedAt }

// StoredAwayUntil is away mode's end as stored, past or not. Stores only.
func (a *Account) StoredAwayUntil() *time.Time { return a.s.AwayUntil }

// StoredStatus is the status as stored, which Status may read as active once
// a suspension has lapsed. Stores only.
func (a *Account) StoredStatus() Status { return a.s.Status }

// Saved records that a store wrote the account under writeID. Stores only.
func (a *Account) Saved(writeID uuid.UUID) { a.s.WriteID = writeID }

// Status reports the status in force at now. A suspension whose end has
// passed reads as active even though the stored row still says suspended.
func (a *Account) Status(now time.Time) Status {
	if a.s.Status == StatusSuspended && a.s.SuspendedUntil != nil && !now.Before(*a.s.SuspendedUntil) {
		return StatusActive
	}
	return a.s.Status
}

// Suspended reports whether a suspension is in force at now.
func (a *Account) Suspended(now time.Time) bool { return a.Status(now) == StatusSuspended }

// Refusal is the error a write by an account in this status answers: nil
// while active, ErrSuspended while suspended, and ErrSessionRevoked once
// deleted, since a deleted account's sessions are revoked and only an
// access token that has not expired yet can still call.
func (s Status) Refusal() *apperr.Error {
	switch s {
	case StatusSuspended:
		return ErrSuspended
	case StatusDeleted:
		return ErrSessionRevoked
	case StatusActive:
	default: // the CHECK on accounts.status allows no other status
	}
	return nil
}

// CanWrite returns the refusal of the status in force: ErrSuspended while a
// suspension is, ErrSessionRevoked once deleted. Every use case that writes
// loads the account and calls it; access tokens stay valid for up to 15
// minutes after a suspension or a deletion, so this is what stops writes
// at once.
func (a *Account) CanWrite(now time.Time) error {
	if r := a.Status(now).Refusal(); r != nil {
		return r
	}
	return nil
}

// Edit sets the display name and the bio, each when not nil. Unlike New,
// it does not accept an empty name; the bio may be empty. A field that
// breaks its rule is named in ErrInvalidAccount and nothing changes.
func (a *Account) Edit(displayName, bio *string, now time.Time) error {
	e := ErrInvalidAccount
	name, about := a.s.DisplayName, a.s.Bio
	if displayName != nil {
		name = strings.TrimSpace(*displayName)
		if n := utf8.RuneCountInString(name); n < 1 || n > maxNameRunes {
			e = e.WithField("display_name", apperr.Length(n, 1, maxNameRunes), "1 to 50 characters")
		}
	}
	if bio != nil {
		about = strings.TrimSpace(*bio)
		if utf8.RuneCountInString(about) > maxBioRunes {
			e = e.WithField("bio", apperr.TooLong, "at most 200 characters")
		}
	}
	if len(e.Fields) > 0 {
		return e
	}
	a.s.DisplayName, a.s.Bio, a.s.UpdatedAt = name, about, now
	return nil
}

// SetAvatar sets the object key of the avatar; an empty key removes it.
func (a *Account) SetAvatar(key string, now time.Time) {
	a.s.AvatarKey, a.s.UpdatedAt = key, now
}

// SetAway turns away mode on until until, which must lie after now and at
// most MaxAway ahead.
func (a *Account) SetAway(until, now time.Time) error {
	if !until.After(now) {
		return ErrInvalidAccount.WithField("away_until", apperr.OutOfRange, "must be in the future")
	}
	if until.Sub(now) > MaxAway {
		return ErrInvalidAccount.WithField("away_until", apperr.OutOfRange, "at most 30 days ahead")
	}
	a.s.AwayUntil, a.s.UpdatedAt = &until, now
	return nil
}

// ClearAway turns away mode off.
func (a *Account) ClearAway(now time.Time) { a.s.AwayUntil, a.s.UpdatedAt = nil, now }

// AwayUntil is when away mode ends, or nil when it is not in force at now.
func (a *Account) AwayUntil(now time.Time) *time.Time {
	if a.s.AwayUntil == nil || !now.Before(*a.s.AwayUntil) {
		return nil
	}
	return a.s.AwayUntil
}

// Away reports whether away mode is in force at now.
func (a *Account) Away(now time.Time) bool { return a.AwayUntil(now) != nil }

// Suspend stops the account from writing, until the given time or, with a
// nil until, until Unsuspend.
func (a *Account) Suspend(reason string, until *time.Time, now time.Time) error {
	if a.s.Status == StatusDeleted {
		return ErrAccountNotFound
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrSuspendReason
	}
	if until != nil && !until.After(now) {
		return ErrSuspendUntil
	}
	a.s.Status = StatusSuspended
	a.s.SuspendReason = reason
	a.s.SuspendedUntil = until
	a.s.UpdatedAt = now
	return nil
}

// Unsuspend lifts a suspension in force. A deleted account answers
// ErrAccountNotFound, as Suspend does; one not suspended reports false.
func (a *Account) Unsuspend(now time.Time) (bool, error) {
	if a.s.Status == StatusDeleted {
		return false, ErrAccountNotFound
	}
	if !a.Suspended(now) {
		return false, nil
	}
	a.s.Status = StatusActive
	a.s.SuspendReason = ""
	a.s.SuspendedUntil = nil
	a.s.UpdatedAt = now
	return true, nil
}

// Delete ends the account: its name, bio and avatar go and it writes no
// more. A suspension in force refuses it, so deleting cannot shed one.
// Deleting a deleted account changes nothing and reports false.
func (a *Account) Delete(now time.Time) (bool, error) {
	if a.s.Status == StatusDeleted {
		return false, nil
	}
	if a.Suspended(now) {
		return false, ErrSuspended
	}
	a.s.Status, a.s.DeletedAt, a.s.UpdatedAt = StatusDeleted, &now, now
	a.s.DisplayName, a.s.Bio, a.s.AvatarKey, a.s.AwayUntil = "", "", "", nil
	a.s.SuspendReason, a.s.SuspendedUntil = "", nil
	return true, nil
}
