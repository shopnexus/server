// Package auth proves who a person is: OTP sign-in, sessions and tokens.
package auth

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/handler"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

// Settings is what auth's Mount takes beyond module.Deps.
type Settings struct {
	Keys  config.AuthConfig
	SMS   sms.Sender
	Email email.Sender
}

// Protect returns the middleware that requires a signed-in caller. It needs
// only the keys, so app builds it before the modules that use it.
func Protect(d module.Deps, keys config.AuthConfig) func(http.Handler) http.Handler {
	return handler.Protect(domain.NewVerifier(keys), d.Now, d.Log)
}

// Mounted is what auth's Mount gives app and other modules.
type Mounted struct {
	Services []restate.ServiceDefinition
	Service  service.Service
}

// Mount registers auth's HTTP handlers and returns its Restate services, with
// prefixed names, and the Service other modules read identities through.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service) Mounted {
	svc := service.New(service.Deps{
		Challenges: repository.NewChallenges(d.Pool), Quotas: repository.NewQuotas(d.Pool),
		Credentials: repository.NewCredentials(d.Pool), Sessions: repository.NewSessions(d.Pool),
		Codes: domain.NewCodes(s.Keys.OTPKey), Signer: domain.NewSigner(s.Keys), Accounts: accounts,
		SMS: s.SMS, Email: s.Email, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, Protect(d, s.Keys))
	return Mounted{Services: svc.Durable(), Service: svc}
}
