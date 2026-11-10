// Package account owns who a person is and how they prove it: display name,
// bio, avatar, status and suspension, blocks and addresses, and sign-in with a
// phone number Firebase verified, a Google account or an email code, with its
// sessions and tokens.
package account

import (
	"net/http"
	"time"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/handler"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/email"
)

// Settings is what account's Mount takes beyond module.Deps.
type Settings struct {
	Keys   config.AuthConfig
	Phones service.PhoneVerifier
	// Email mails sign-in codes.
	Email email.Sender
}

// Guards returns the middlewares app puts in module.Deps: protect requires a
// signed-in caller, identify lets anyone through and tells the handler who
// signed in. They need only the keys, so app builds them before any module.
func Guards(d module.Deps, keys config.AuthConfig) (protect, identify func(http.Handler) http.Handler) {
	v := domain.NewVerifier(keys)
	return handler.Protect(v, d.Now, d.Log), handler.Identify(v, d.Now, d.Log)
}

// TokenVerifier checks a customer access token as Protect does. The
// WebSocket checks with it the tokens a connection renews itself with.
func TokenVerifier(keys config.AuthConfig) func(token string, now time.Time) (authn.Principal, error) {
	return domain.NewVerifier(keys).Verify
}

// Mount registers account's HTTP handlers and returns its Restate services
// and the Service other modules call. media makes avatars.
func Mount(d module.Deps, s Settings, media mediasvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Accounts: repository.NewAccounts(d.Pool), Follows: repository.NewFollows(d.Pool), Addresses: repository.NewAddresses(d.Pool), Credentials: repository.NewCredentials(d.Pool),
		Sessions: repository.NewSessions(d.Pool), Signer: domain.NewSigner(s.Keys), Phones: s.Phones, Media: media,
		Challenges: repository.NewChallenges(d.Pool), Quotas: repository.NewQuotas(d.Pool), Codes: domain.NewCodes(s.Keys.OTPKey), Email: s.Email,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	return module.Mounted[service.Service]{Services: svc.Durable(), Service: svc}
}
