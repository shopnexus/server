// Package domain holds the account entity and its rules.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

// Status is whether an account may act.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
)

const (
	maxNameRunes = 50
	maxBioRunes  = 200
)

// Account is one person. Fields are unexported so every change goes through
// a method that keeps the rules.
type Account struct {
	id             uuid.UUID
	displayName    string
	bio            string
	avatarKey      string // the object key of the avatar; empty without one
	status         Status
	suspendedUntil *time.Time
	suspendReason  string
	writeID        uuid.UUID
	createdAt      time.Time
	updatedAt      time.Time
}

// New builds an active account with the caller's id. The display name may
// be empty until the person completes their profile. The first write ID is
// the id.
func New(id uuid.UUID, displayName string, now time.Time) (Account, error) {
	name := strings.TrimSpace(displayName)
	if utf8.RuneCountInString(name) > maxNameRunes {
		return Account{}, ErrInvalidAccount.WithField("display_name", "at most 50 characters")
	}
	return Account{id: id, displayName: name, status: StatusActive, writeID: id, createdAt: now, updatedAt: now}, nil
}

// RestoreAccount rebuilds a stored account. Stores only.
func RestoreAccount(id uuid.UUID, displayName, bio, avatarKey string, status Status, suspendedUntil *time.Time,
	suspendReason string, writeID uuid.UUID, createdAt, updatedAt time.Time,
) Account {
	return Account{
		id: id, displayName: displayName, bio: bio, avatarKey: avatarKey, status: status,
		suspendedUntil: suspendedUntil, suspendReason: suspendReason, writeID: writeID, createdAt: createdAt, updatedAt: updatedAt,
	}
}

func (a *Account) ID() uuid.UUID              { return a.id }
func (a *Account) DisplayName() string        { return a.displayName }
func (a *Account) Bio() string                { return a.bio }
func (a *Account) AvatarKey() string          { return a.avatarKey }
func (a *Account) SuspendReason() string      { return a.suspendReason }
func (a *Account) SuspendedUntil() *time.Time { return a.suspendedUntil }
func (a *Account) CreatedAt() time.Time       { return a.createdAt }
func (a *Account) UpdatedAt() time.Time       { return a.updatedAt }
func (a *Account) WriteID() uuid.UUID         { return a.writeID }

// StoredStatus is the status as stored, which Status may read as active once
// a suspension has lapsed. Stores only.
func (a *Account) StoredStatus() Status { return a.status }

// Saved records that a store wrote the account under writeID. Stores only.
func (a *Account) Saved(writeID uuid.UUID) { a.writeID = writeID }

// Status reports the status in force at now. A suspension whose end has
// passed reads as active even though the stored row still says suspended.
func (a *Account) Status(now time.Time) Status {
	if a.status == StatusSuspended && a.suspendedUntil != nil && !now.Before(*a.suspendedUntil) {
		return StatusActive
	}
	return a.status
}

// CanWrite returns ErrSuspended while a suspension is in force. Every use
// case that writes loads the account and calls it; access tokens stay valid
// for up to 15 minutes after a suspension, so this is what stops writes at
// once.
func (a *Account) CanWrite(now time.Time) error {
	if a.Status(now) == StatusSuspended {
		return ErrSuspended
	}
	return nil
}

// Edit sets the display name and the bio, each when not nil. Unlike New,
// it does not accept an empty name; the bio may be empty. A field that
// breaks its rule is named in ErrInvalidAccount and nothing changes.
func (a *Account) Edit(displayName, bio *string, now time.Time) error {
	e := ErrInvalidAccount
	name, about := a.displayName, a.bio
	if displayName != nil {
		name = strings.TrimSpace(*displayName)
		if n := utf8.RuneCountInString(name); n < 1 || n > maxNameRunes {
			e = e.WithField("display_name", "1 to 50 characters")
		}
	}
	if bio != nil {
		about = strings.TrimSpace(*bio)
		if utf8.RuneCountInString(about) > maxBioRunes {
			e = e.WithField("bio", "at most 200 characters")
		}
	}
	if len(e.Fields) > 0 {
		return e
	}
	a.displayName, a.bio, a.updatedAt = name, about, now
	return nil
}

// SetAvatar sets the object key of the avatar; an empty key removes it.
func (a *Account) SetAvatar(key string, now time.Time) {
	a.avatarKey, a.updatedAt = key, now
}

// Suspend stops the account from writing, until the given time or, with a
// nil until, until Reinstate.
func (a *Account) Suspend(reason string, until *time.Time, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrSuspendReason
	}
	if until != nil && !until.After(now) {
		return ErrSuspendUntil
	}
	a.status = StatusSuspended
	a.suspendReason = reason
	a.suspendedUntil = until
	a.updatedAt = now
	return nil
}

// Reinstate lifts a suspension.
func (a *Account) Reinstate(now time.Time) {
	a.status = StatusActive
	a.suspendReason = ""
	a.suspendedUntil = nil
	a.updatedAt = now
}
