package repository

import (
	"context"
	"time"

	"github.com/shopnexus/server2/internal/platform/audit"
)

// Locks counts failed sign-ins per email and locks the email after
// domain.MaxFailures in a row. Unknown emails count exactly like members'
// emails, so a lock tells nothing about which emails are staff.
//
// Every sign-in call charges its attempt before checking anything, so
// concurrent guesses cannot pass the check together. The call then takes
// the charge back with Refund when the password was right, keeps it with
// Fail when the guess was wrong, or forgets every failure with Clear after
// a finished sign-in.
type Locks interface {
	// Charge counts one attempt on key in one statement. While the email is
	// locked, or domain.MaxFailures attempts are counted already, it counts nothing
	// and returns domain.ErrLoginLocked with the seconds to wait.
	Charge(ctx context.Context, key []byte, now time.Time) error
	Refund(ctx context.Context, key []byte, now time.Time) error
	// Fail keeps the charge and, once domain.MaxFailures are counted, locks the
	// email for domain.LockFor.
	Fail(ctx context.Context, key []byte, now time.Time) error
	Clear(ctx context.Context, key []byte, now time.Time) error
	// Unlock is Clear for an admin or the CLI, recorded as e in the same
	// transaction.
	Unlock(ctx context.Context, key []byte, now time.Time, e audit.Entry) error
	// LockedUntil is a query: for each of keys locked at now, the end of the
	// lock, keyed by string(key).
	LockedUntil(ctx context.Context, keys [][]byte, now time.Time) (map[string]time.Time, error)
}
