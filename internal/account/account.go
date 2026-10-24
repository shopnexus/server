// Package account owns who a person is: display name, status and suspension.
// It knows nothing about signing in; package auth does.
package account

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

const maxNameRunes = 50

// Account is one person. Fields are unexported so every change goes through
// a method that keeps the rules; the store in this package reads them
// directly.
type Account struct {
	id             uuid.UUID
	displayName    string
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
	name, err := cleanName(displayName, true)
	if err != nil {
		return Account{}, err
	}
	return Account{id: id, displayName: name, status: StatusActive, writeID: id, createdAt: now, updatedAt: now}, nil
}

func (a *Account) ID() uuid.UUID              { return a.id }
func (a *Account) DisplayName() string        { return a.displayName }
func (a *Account) SuspendReason() string      { return a.suspendReason }
func (a *Account) SuspendedUntil() *time.Time { return a.suspendedUntil }
func (a *Account) CreatedAt() time.Time       { return a.createdAt }
func (a *Account) UpdatedAt() time.Time       { return a.updatedAt }
func (a *Account) WriteID() uuid.UUID         { return a.writeID }

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

// SetDisplayName replaces the name. Unlike New, it does not accept an empty
// one.
func (a *Account) SetDisplayName(name string, now time.Time) error {
	clean, err := cleanName(name, false)
	if err != nil {
		return err
	}
	a.displayName = clean
	a.updatedAt = now
	return nil
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

func cleanName(name string, allowEmpty bool) (string, error) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n > maxNameRunes || (n == 0 && !allowEmpty) {
		return "", ErrInvalidName
	}
	return name, nil
}
