// Package auth proves who a person is: OTP sign-in, sessions and tokens. Use
// cases are functions in this package; otp, credentials and session hold the
// aggregates.
package auth

import (
	"net/http"
	"net/netip"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

// Settings is what auth's Mount takes beyond module.Deps.
type Settings struct {
	Keys           config.AuthConfig
	TrustedProxies []netip.Prefix
	SMS            sms.Sender
	Email          email.Sender
}

// Protect returns the middleware that requires a signed-in caller. It needs
// only the keys, so app builds it before the modules that use it.
func Protect(d module.Deps, keys config.AuthConfig) func(http.Handler) http.Handler {
	return RequireAuth(session.NewVerifier(keys), d.Now, d.Log)
}

// Mounted is what auth's Mount gives app and other modules.
type Mounted struct {
	Services []restate.ServiceDefinition
	Client   Client
}

// Mount registers auth's HTTP handlers and returns its Restate services, with
// prefixed names, and the client other modules read identities through.
func Mount(d module.Deps, s Settings, accounts AccountPort) Mounted {
	challenges := otp.NewChallengePGStore(d.Pool)
	quotas := otp.NewQuotaPGStore(d.Pool)
	creds := credentials.NewPGStore(d.Pool)
	sessions := session.NewPGStore(d.Pool)
	codes := otp.NewCodes(s.Keys.OTPKey)
	signer := session.NewSigner(s.Keys)
	rs := Restate{Client: d.Ingress, Prefix: d.ServicePrefix}

	r, now, log := d.Router, d.Now, d.Log
	r.Handle("POST /v1/auth/otp", OTPSendHandler(rs, s.TrustedProxies, log))
	r.Handle("POST /v1/auth/otp/verify", SignInHandler(rs, challenges, codes, sessions, signer, now, log))
	r.Handle("POST /v1/auth/refresh", RefreshHandler(sessions, accounts, signer, now, log))
	protect := Protect(d, s.Keys)
	r.Handle("POST /v1/auth/logout", protect(LogoutHandler(sessions, now, log)))
	r.Handle("POST /v1/auth/logout-all", protect(LogoutAllHandler(sessions, now, log)))
	r.Handle("GET /v1/auth/sessions", protect(ListSessionsHandler(sessions, now, log)))
	r.Handle("DELETE /v1/auth/sessions/{id}", protect(RemoveSessionHandler(sessions, now, log)))

	return Mounted{
		Services: []restate.ServiceDefinition{
			restate.Reflect(OTP{
				Prefix: d.ServicePrefix, Quotas: quotas, Challenges: challenges, Codes: codes, SMS: s.SMS, Email: s.Email,
				Now: now,
			}),
			restate.Reflect(SignIn{
				Prefix: d.ServicePrefix, Challenges: challenges, Credentials: creds, Accounts: accounts,
				Now: now,
			}),
		},
		Client: Client{Credentials: creds},
	}
}
