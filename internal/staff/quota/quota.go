// Package quota locks a staff sign-in email after five failures in a row.
// Unknown emails count exactly like members' emails, so a lock tells nothing
// about which emails are staff. The key is an HMAC of the email.
package quota

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"time"
	"uuid"
)

const (
	MaxFailures = 5
	LockFor     = 15 * time.Minute
)

var ErrConflict = errors.New("staff login quota: changed meanwhile")

// Quota counts failed sign-ins on one email.
type Quota struct {
	key         []byte
	failures    int
	lockedUntil *time.Time
	updatedAt   time.Time
	writeID     uuid.UUID // zero until first saved
}

// Key is HMAC-SHA256 of the normalised email under the staff quota key.
func Key(mac []byte, email string) []byte {
	h := hmac.New(sha256.New, mac)
	h.Write([]byte("staff-login:" + email))
	return h.Sum(nil)
}

// New is the quota of an email that has never failed.
func New(key []byte) Quota {
	return Quota{key: key, failures: 0, lockedUntil: nil, updatedAt: time.Time{}, writeID: uuid.UUID{}}
}

func (q *Quota) Key() []byte   { return q.key }
func (q *Quota) Failures() int { return q.failures }

// Locked reports whether the email is locked at now and for how long.
func (q *Quota) Locked(now time.Time) (time.Duration, bool) {
	if q.lockedUntil != nil && now.Before(*q.lockedUntil) {
		return q.lockedUntil.Sub(now), true
	}
	return 0, false
}

// Fail counts a failure. The fifth locks the email for LockFor and starts
// the count again; Fail reports whether it locked.
func (q *Quota) Fail(now time.Time) bool {
	q.failures++
	q.updatedAt = now
	if q.failures < MaxFailures {
		return false
	}
	until := now.Add(LockFor)
	q.lockedUntil, q.failures = &until, 0
	return true
}

// Clear forgets the failures and any lock.
func (q *Quota) Clear(now time.Time) {
	q.failures, q.lockedUntil, q.updatedAt = 0, nil, now
}
