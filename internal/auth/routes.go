// Package auth proves who a person is: OTP sign-in, sessions and tokens. Use
// cases are functions in this package; otp, credentials and session hold the
// aggregates.
package auth

import (
	"log/slog"
	"net/netip"
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

// Ports holds the adapters auth's handlers and services use. Only Routes,
// Services and app.go read it.
type Ports struct {
	Accounts    AccountPort
	Challenges  otp.ChallengeStore
	Quotas      otp.QuotaStore
	Credentials credentials.Store
	Sessions    session.Store
	SMS         sms.Sender
	Email       email.Sender
}

// Keys holds what auth derives codes and signs tokens with.
type Keys struct {
	Codes    otp.Codes
	Signer   session.Signer
	Verifier session.Verifier
}

// Services returns auth's Restate services with prefixed names.
func Services(prefix string, p Ports, k Keys) []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(OTP{Prefix: prefix, Quotas: p.Quotas, Challenges: p.Challenges, Codes: k.Codes, SMS: p.SMS, Email: p.Email}),
		restate.Reflect(SignIn{Prefix: prefix, Challenges: p.Challenges, Credentials: p.Credentials, Accounts: p.Accounts}),
	}
}

// Routes registers auth's HTTP handlers.
func Routes(r *httpx.Router, p Ports, k Keys, rs Restate, trusted []netip.Prefix, now func() time.Time, log *slog.Logger) {
	r.Handle("POST /v1/auth/otp", OTPSendHandler(rs, trusted, log))
	r.Handle("POST /v1/auth/otp/verify", SignInHandler(rs, p.Challenges, k.Codes, p.Sessions, k.Signer, now, log))
}
