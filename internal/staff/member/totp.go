package member

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP, which every authenticator app implements, is HMAC-SHA1.
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"
)

const (
	totpPeriod = 30
	secretLen  = 20
	issuer     = "shopnexus staff"
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns 20 random bytes, the size RFC 4226 recommends.
func NewTOTPSecret() []byte {
	b := make([]byte, secretLen)
	rand.Read(b)
	return b
}

// EncodeSecret is the base32 form an authenticator takes by hand.
func EncodeSecret(secret []byte) string { return base32NoPad.EncodeToString(secret) }

// TOTPURI is the otpauth URI a QR code carries.
func TOTPURI(secret []byte, email string) string {
	q := url.Values{
		"secret": {EncodeSecret(secret)}, "issuer": {issuer},
		"algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"},
	}
	return "otpauth://totp/" + url.PathEscape(issuer+":"+email) + "?" + q.Encode()
}

// Code is the six-digit code for secret at t.
func Code(secret []byte, t time.Time) string { return codeAt(secret, t.Unix()/totpPeriod) }

func codeAt(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // A time step since 1970 is never negative.
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", bin%1_000_000)
}

// matchCode accepts code for the step before, at or after now's, when that
// step is past last, and returns the step it matched.
func matchCode(secret []byte, code string, now time.Time, last int64) (int64, bool) {
	cur := now.Unix() / totpPeriod
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if s > last && hmac.Equal([]byte(codeAt(secret, s)), []byte(code)) {
			return s, true
		}
	}
	return 0, false
}
