// Package contact normalises phone numbers and email addresses. Identity and
// OTP rows store the normalised form, so UNIQUE (kind, value) sees one value
// per number however a person typed it.
package contact

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

type Kind string

const (
	Phone Kind = "phone"
	Email Kind = "email"
)

var (
	ErrInvalidPhone = apperr.New(apperr.KindInvalid, "invalid_phone", "phone must be a Vietnamese mobile number")
	ErrInvalidEmail = apperr.New(apperr.KindInvalid, "invalid_email", "email address is not valid")
)

// Address is a normalised phone number or email address. Only the Parse
// functions, Restore and UnmarshalJSON build one.
type Address struct {
	kind  Kind
	value string
}

type addressJSON struct {
	Kind  Kind   `json:"kind"`
	Value string `json:"value"`
}

// MarshalJSON lets a Restate step return an Address.
func (a Address) MarshalJSON() ([]byte, error) { return json.Marshal(addressJSON{a.kind, a.value}) }

// UnmarshalJSON reads what MarshalJSON wrote; the value was normalised then.
func (a *Address) UnmarshalJSON(b []byte) error {
	var j addressJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*a = Address{j.Kind, j.Value}
	return nil
}

// vnMobile is +84, a mobile prefix (3, 5, 7, 8, 9) and eight digits. Other
// countries arrive in a later phase.
var vnMobile = regexp.MustCompile(`^\+84[35789]\d{8}$`)

// ParsePhone accepts 0901234567, 84901234567, +84 90 123 4567 and the
// country code before the trunk 0 (+84 090 123 4567), ignoring spaces, dots,
// dashes and parentheses, and returns +84901234567.
func ParsePhone(raw string) (Address, error) {
	s := strings.Map(func(r rune) rune {
		if r == ' ' || r == '.' || r == '-' || r == '(' || r == ')' {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(s, "+840"):
		s = "+84" + s[4:]
	case strings.HasPrefix(s, "+"):
	case strings.HasPrefix(s, "84") && len(s) == 11:
		s = "+" + s
	case strings.HasPrefix(s, "0") && len(s) == 10:
		s = "+84" + s[1:]
	default:
		// vnMobile rejects whatever else is left.
	}
	if !vnMobile.MatchString(s) {
		return Address{}, ErrInvalidPhone
	}
	return Address{Phone, s}, nil
}

// ParseEmail lower-cases the address and rejects display names and domains
// without a dot.
func ParseEmail(raw string) (Address, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || a.Address != s || len(s) > 254 {
		return Address{}, ErrInvalidEmail
	}
	local, domain, _ := strings.Cut(s, "@")
	if local == "" || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return Address{}, ErrInvalidEmail
	}
	return Address{Email, s}, nil
}

// Parse dispatches on kind.
func Parse(kind Kind, raw string) (Address, error) {
	switch kind {
	case Phone:
		return ParsePhone(raw)
	case Email:
		return ParseEmail(raw)
	}
	return Address{}, fmt.Errorf("contact: unknown kind %q", kind)
}

// Restore rebuilds an address that was parsed before it was stored or sent.
func Restore(kind Kind, value string) Address { return Address{kind, value} }

func (a Address) Kind() Kind     { return a.kind }
func (a Address) Value() string  { return a.value }
func (a Address) IsZero() bool   { return a.value == "" }
func (a Address) String() string { return a.Masked() }

// Masked shows enough for a person to recognise their own address:
// +8490****567, l***@gmail.com.
func (a Address) Masked() string {
	switch a.kind {
	case Phone:
		return a.value[:5] + "****" + a.value[len(a.value)-3:]
	case Email:
		local, domain, _ := strings.Cut(a.value, "@")
		return local[:1] + "***@" + domain
	}
	return ""
}

// LogValue keeps full addresses out of logs.
func (a Address) LogValue() slog.Value { return slog.StringValue(a.Masked()) }
