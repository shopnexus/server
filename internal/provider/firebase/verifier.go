package firebase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/contact"
)

const (
	// freshFor is how long after the verification a token signs in.
	freshFor = 5 * time.Minute
	skew     = 30 * time.Second
)

// ErrInvalidToken is a token that failed a check; the log says which.
var ErrInvalidToken = apperr.New(apperr.KindUnauthenticated, "invalid_phone_token",
	"the phone token is not valid; verify the number again")

// PhoneVerifier checks phone tokens of one Firebase project.
type PhoneVerifier struct {
	cfg  config.FirebaseConfig
	auth *keySet
	pnv  *keySet
	now  func() time.Time
	log  *slog.Logger
}

func NewPhoneVerifier(cfg config.FirebaseConfig, now func() time.Time, log *slog.Logger) *PhoneVerifier {
	client := &http.Client{Timeout: fetchTimeout}
	return &PhoneVerifier{
		cfg: cfg, now: now, log: log,
		auth: &keySet{url: cfg.AuthJWKSURL, http: client, now: now, log: log},
		pnv:  &keySet{url: cfg.PNVJWKSURL, http: client, now: now, log: log},
	}
}

// smsClaims is an ID token of Firebase Authentication.
type smsClaims struct {
	jwt.RegisteredClaims

	AuthTime    int64  `json:"auth_time"`
	PhoneNumber string `json:"phone_number"`
	Firebase    struct {
		SignInProvider string `json:"sign_in_provider"`
	} `json:"firebase"`
}

// simClaims is a Phone Number Verification token.
type simClaims struct {
	jwt.RegisteredClaims

	TestNumber bool `json:"test_number"`
}

// Verify returns the phone number token proves. The token's iss picks the
// key set; the token must then carry that issuer's algorithm, audience and
// a verification less than 5 minutes old. A failed check is ErrInvalidToken,
// keys that cannot be fetched are ErrUnavailable, and a number outside
// Vietnam is contact.ErrInvalidPhone.
func (v *PhoneVerifier) Verify(ctx context.Context, token string) (contact.Address, error) {
	refuse := func(why string, err error) (contact.Address, error) {
		if errors.Is(err, ErrUnavailable) {
			return contact.Address{}, ErrUnavailable
		}
		v.log.InfoContext(ctx, "phone token refused", "why", why, "err", err)
		return contact.Address{}, ErrInvalidToken
	}
	var unverified jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(token, &unverified); err != nil {
		return refuse("malformed", err)
	}
	now := v.now()
	keyIn := func(set *keySet) jwt.Keyfunc {
		return func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string) //nolint:revive // a token without a kid finds no key
			return set.key(ctx, kid)
		}
	}
	parser := func(alg, aud string) *jwt.Parser {
		return jwt.NewParser(jwt.WithValidMethods([]string{alg}), jwt.WithIssuer(unverified.Issuer),
			jwt.WithAudience(aud), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(skew),
			jwt.WithTimeFunc(v.now))
	}
	switch unverified.Issuer {
	case "https://securetoken.google.com/" + v.cfg.ProjectID:
		var c smsClaims
		if _, err := parser("RS256", v.cfg.ProjectID).ParseWithClaims(token, &c, keyIn(v.auth)); err != nil {
			return refuse("sms", err)
		}
		verified := time.Unix(c.AuthTime, 0)
		switch {
		case c.AuthTime == 0 || now.Sub(verified) > freshFor || verified.After(now.Add(skew)):
			return refuse("sms: not verified in the last 5 minutes", nil)
		case c.Firebase.SignInProvider != "phone" || c.PhoneNumber == "":
			return refuse("sms: not a phone sign-in", nil)
		}
		a, err := contact.ParsePhone(c.PhoneNumber)
		if err != nil {
			return contact.Address{}, fmt.Errorf("the verified number: %w", err)
		}
		return a, nil
	case "https://fpnv.googleapis.com/projects/" + v.cfg.ProjectNumber:
		var c simClaims
		t, err := parser("ES256", "https://fpnv.googleapis.com/projects/"+v.cfg.ProjectID).ParseWithClaims(token, &c, keyIn(v.pnv))
		if err != nil {
			return refuse("sim", err)
		}
		switch {
		case t.Header["typ"] != "JWT":
			return refuse("sim: typ is not JWT", nil)
		case c.IssuedAt == nil || now.Sub(c.IssuedAt.Time) > freshFor:
			return refuse("sim: not issued in the last 5 minutes", nil)
		case c.Subject == "":
			return refuse("sim: no number", nil)
		case c.TestNumber && !v.cfg.TestNumbers:
			return refuse("sim: a test number", nil)
		case c.TestNumber:
			// A test number is the country code and zeros, which no mobile
			// prefix matches; it is kept as it comes.
			return contact.Restore(contact.Phone, c.Subject), nil
		}
		a, err := contact.ParsePhone(c.Subject)
		if err != nil {
			return contact.Address{}, fmt.Errorf("the verified number: %w", err)
		}
		return a, nil
	default:
		return refuse("unknown issuer", fmt.Errorf("iss %q", unverified.Issuer))
	}
}
