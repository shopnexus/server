// Package auth proves who a person is: sign-in with a phone number Firebase
// verified, sessions and tokens.
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
)

// Settings is what auth's Mount takes beyond module.Deps.
type Settings struct {
	Keys   config.AuthConfig
	Phones service.PhoneVerifier
}

// Protect returns the middleware that requires a signed-in caller. It needs
// only the keys, so app builds it before the modules that use it.
func Protect(d module.Deps, keys config.AuthConfig) func(http.Handler) http.Handler {
	return handler.Protect(domain.NewVerifier(keys), d.Now, d.Log)
}

// Identify returns the middleware for a route anyone may call: a caller
// who sends a token must send a valid one, and the handler sees them.
func Identify(d module.Deps, keys config.AuthConfig) func(http.Handler) http.Handler {
	return handler.Identify(domain.NewVerifier(keys), d.Now, d.Log)
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
		Credentials: repository.NewCredentials(d.Pool), Sessions: repository.NewSessions(d.Pool),
		Signer: domain.NewSigner(s.Keys), Accounts: accounts, Phones: s.Phones,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, Protect(d, s.Keys))
	return Mounted{Services: svc.Durable(), Service: svc}
}
