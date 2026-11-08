// Package account owns who a person is and how they prove it: display name,
// bio, avatar, status and suspension, blocks and addresses, and sign-in with a
// phone number Firebase verified, with its sessions and tokens.
package account

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/handler"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Settings is what account's Mount takes beyond module.Deps.
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

// Mounted is what account's Mount gives app and other modules.
type Mounted struct {
	Services []restate.ServiceDefinition
	Service  service.Service
}

// Mount registers account's HTTP handlers, the signed-in ones behind
// protect, and returns its Restate services, with prefixed names, and the
// Service other modules call. media makes avatars.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, s Settings, media mediasvc.Service) Mounted {
	svc := service.New(service.Deps{
		Accounts: repository.NewAccounts(d.Pool), Credentials: repository.NewCredentials(d.Pool),
		Sessions: repository.NewSessions(d.Pool), Signer: domain.NewSigner(s.Keys), Phones: s.Phones, Media: media,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, protect)
	return Mounted{Services: svc.Durable(), Service: svc}
}
