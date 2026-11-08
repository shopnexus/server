// Package service holds module account's use cases: accounts, suspensions,
// blocks and addresses, and signing in with a phone number Firebase verified,
// with the sessions and tokens it starts. Signing in runs on Restate, in
// sign_in_durable.go; Account reaches it through restatex.Caller. Its
// handlers and the modules that call account use Service.
package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// Service is module account's contract.
type Service interface {
	AddressBook
	Sessions

	// Get reads an account.
	Get(ctx context.Context, id uuid.UUID) (AccountView, error)
	// GetMany reads the accounts among ids, keyed by ID; an unknown ID is
	// left out.
	GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]AccountView, error)
	// Update changes the display name, bio and avatar of an account that
	// may write.
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (AccountView, error)
	// Status is the status in force now.
	Status(ctx context.Context, id uuid.UUID) (domain.Status, error)
	// Record reads one account as console members see it.
	Record(ctx context.Context, id uuid.UUID) (Record, error)
	// Records lists accounts newest first: the p.Fetch() rows read, for the
	// caller to cut into a page.
	Records(ctx context.Context, p page.Request) ([]Record, error)
	// Suspend stops an account from writing and records account.suspend.
	Suspend(ctx context.Context, in SuspendInput) (Record, error)
	// Reinstate lifts a suspension and records account.unsuspend.
	Reinstate(ctx context.Context, id uuid.UUID, by audit.Actor) (Record, error)
	// Block stops blocked and blocker from reaching each other; blocking
	// again changes nothing.
	Block(ctx context.Context, blocker, blocked uuid.UUID) error
	Unblock(ctx context.Context, blocker, blocked uuid.UUID) error
	// Blocked reports a block between a and b, in either direction.
	Blocked(ctx context.Context, a, b uuid.UUID) (bool, error)
	// Blocks lists the accounts blocker blocks, newest block first.
	Blocks(ctx context.Context, blocker uuid.UUID, p page.Request) (page.Page[BlockView], error)
	// FindByPhone returns the account holding the phone number q.
	FindByPhone(ctx context.Context, q string) (uuid.UUID, bool, error)
	// Identities lists an account's sign-in methods.
	Identities(ctx context.Context, accountID uuid.UUID) ([]Identity, error)
}

// Sessions is the part of Service that signs people in and keeps their
// sessions.
type Sessions interface {
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
}

// PhoneVerifier checks a token from Firebase and returns the phone number it
// proves: provider/firebase in the app, a fake in tests.
type PhoneVerifier interface {
	Verify(ctx context.Context, token string) (contact.Address, error)
}

// Deps is what New builds Account from. Media makes avatars and their URLs.
type Deps struct {
	Accounts    repository.Accounts
	Credentials repository.Credentials
	Sessions    repository.Sessions
	Signer      domain.Signer
	Phones      PhoneVerifier
	Media       mediasvc.Service
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
	Log     *slog.Logger
}

// Account implements Service in this process.
type Account struct {
	accounts    repository.Accounts
	credentials repository.Credentials
	sessions    repository.Sessions
	signer      domain.Signer
	phones      PhoneVerifier
	media       mediasvc.Service
	prefix      string
	now         func() time.Time
	log         *slog.Logger
	signIn      restatex.Caller[PhoneSignIn, SignInResult]
}

var _ Service = (*Account)(nil)

// New builds Account from d.
func New(d Deps) *Account {
	return &Account{
		accounts: d.Accounts, credentials: d.Credentials, sessions: d.Sessions, signer: d.Signer, phones: d.Phones,
		media: d.Media, prefix: d.Prefix, now: d.Now, log: d.Log,
		signIn: restatex.Caller[PhoneSignIn, SignInResult]{Ingress: d.Ingress, Service: d.Prefix + signInService, Handler: "Run"},
	}
}

// Durable returns the Restate services Account calls, for Mount to bind.
func (s *Account) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(SignIn{Prefix: s.prefix, Accounts: s.accounts, Credentials: s.credentials, Now: s.now}),
	}
}
