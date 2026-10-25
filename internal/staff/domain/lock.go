package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"time"
)

// A sign-in lock: MaxFailures failed sign-ins in a row lock an email for
// LockFor.
const (
	MaxFailures = 5
	LockFor     = 15 * time.Minute
)

// LockKey is HMAC-SHA256 of an email in contact.NormalizeEmail form under
// the staff lock key.
func LockKey(mac []byte, email string) []byte {
	h := hmac.New(sha256.New, mac)
	h.Write([]byte("staff-login:" + email))
	return h.Sum(nil)
}
