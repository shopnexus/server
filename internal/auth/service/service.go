// Package service holds module auth's use cases: signing in with a phone
// number Firebase verified, sessions and their tokens, and the identities
// other modules read. Signing in runs on Restate, in sign_in_durable.go;
// Auth reaches it through restatex.Caller.
package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// Service is module auth's contract.
type Service interface {
	// SignIn checks a phone token, finds or creates the account and starts
	// a session.
	SignIn(ctx context.Context, in SignInInput) (SignedIn, error)
	// Refresh rotates a refresh token.
	Refresh(ctx context.Context, refreshToken string) (domain.Tokens, error)
	// Logout revokes the caller's session.
	Logout(ctx context.Context, p authn.Principal) error
	// LogoutAll revokes every session of the caller's account.
	LogoutAll(ctx context.Context, p authn.Principal) error
	// RemoveSession signs one of the caller's devices out.
	RemoveSession(ctx context.Context, p authn.Principal, id uuid.UUID) error
	// ListSessions lists the caller's devices, newest first.
	ListSessions(ctx context.Context, p authn.Principal, req page.Request) (page.Page[SessionItem], error)
	// ActiveSessions returns which of ids are neither revoked nor expired;
	// notification pushes only to their tokens.
	ActiveSessions(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)
	// FindAccount returns the account holding the phone number or email q.
	FindAccount(ctx context.Context, q string) (uuid.UUID, bool, error)
	// Identities lists an account's sign-in methods.
	Identities(ctx context.Context, accountID uuid.UUID) ([]Identity, error)
}

// PhoneVerifier checks a token from Firebase and returns the phone number it
// proves: provider/firebase in the app, a fake in tests.
type PhoneVerifier interface {
	Verify(ctx context.Context, token string) (contact.Address, error)
}

// Deps is what New builds Auth from.
type Deps struct {
	Credentials repository.Credentials
	Sessions    repository.Sessions
	Signer      domain.Signer
	Accounts    accountsvc.Service
	Phones      PhoneVerifier
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
	Log     *slog.Logger
}

// Auth implements Service in this process.
type Auth struct {
	credentials repository.Credentials
	sessions    repository.Sessions
	signer      domain.Signer
	accounts    accountsvc.Service
	phones      PhoneVerifier
	prefix      string
	now         func() time.Time
	log         *slog.Logger
	signIn      restatex.Caller[PhoneSignIn, SignInResult]
}

var _ Service = (*Auth)(nil)

func New(d Deps) *Auth {
	return &Auth{
		credentials: d.Credentials, sessions: d.Sessions, signer: d.Signer, accounts: d.Accounts, phones: d.Phones,
		prefix: d.Prefix, now: d.Now, log: d.Log,
		signIn: restatex.Caller[PhoneSignIn, SignInResult]{Ingress: d.Ingress, Service: d.Prefix + signInService, Handler: "Run"},
	}
}

// Durable returns the Restate services Auth calls, for Mount to bind.
func (a *Auth) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(SignIn{Prefix: a.prefix, Credentials: a.credentials, Accounts: a.accounts, Now: a.now}),
	}
}
