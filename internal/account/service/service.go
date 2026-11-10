// Package service holds module account's use cases: accounts, suspensions,
// blocks, follows and addresses, and signing in with a phone number Firebase
// verified, a Google account or an email code, with the sessions and tokens
// it starts. Signing in and mailing codes run on Restate, in the
// *_durable.go files; Account reaches them through restatex.Caller. Its
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
	"github.com/shopnexus/server2/internal/provider/email"
)

// Service is module account's contract.
type Service interface {
	Sessions

	// Get reads an account.
	Get(ctx context.Context, id uuid.UUID) (AccountView, error)
	// GetMany reads the accounts among ids, keyed by ID; an unknown ID is
	// left out.
	GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]AccountView, error)
	// Update changes the display name, bio and avatar of an account that
	// may write.
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (AccountView, error)
	// Status is the account's status in force now: active, suspended (a
	// suspension that has not lapsed) or deleted. A module that refuses a
	// write answers status.Refusal(); a Restate step returns the status, so
	// the flow can return the refusal as data.
	Status(ctx context.Context, id uuid.UUID) (domain.Status, error)
	// Erase deletes the account: the row stays without a name, bio or
	// avatar, its credentials, addresses, blocks and follows go, its
	// sessions are revoked and account.deleted is written. A suspension in
	// force is domain.ErrSuspended; a deleted account changes nothing.
	Erase(ctx context.Context, id uuid.UUID) error
	// Touch records that the person has the app open now.
	Touch(ctx context.Context, id uuid.UUID) error
	// ConsoleAccount reads one account as console members see it.
	ConsoleAccount(ctx context.Context, id uuid.UUID) (ConsoleAccount, error)
	// ConsoleAccounts lists accounts as console members see them, newest first.
	ConsoleAccounts(ctx context.Context, p page.Request) (page.Page[ConsoleAccount], error)
	// Suspend stops an account from writing and records account.suspend.
	Suspend(ctx context.Context, in SuspendInput) (ConsoleAccount, error)
	// Unsuspend lifts a suspension and records account.unsuspend.
	Unsuspend(ctx context.Context, id uuid.UUID, by audit.Actor) (ConsoleAccount, error)
	// Block stops blocked and blocker from reaching each other and deletes
	// the follows between them; blocking again changes nothing.
	Block(ctx context.Context, blocker, blocked uuid.UUID) error
	Unblock(ctx context.Context, blocker, blocked uuid.UUID) error
	// Blocked reports a block between a and b, in either direction.
	Blocked(ctx context.Context, a, b uuid.UUID) (bool, error)
	// Blocks lists the accounts blocker blocks, newest block first.
	Blocks(ctx context.Context, blocker uuid.UUID, p page.Request) (page.Page[BlockView], error)
	// Follow makes follower follow followed; following again changes
	// nothing. A block either way is domain.ErrBlocked.
	Follow(ctx context.Context, follower, followed uuid.UUID) error
	Unfollow(ctx context.Context, follower, followed uuid.UUID) error
	// Following reports whether follower follows followed.
	Following(ctx context.Context, follower, followed uuid.UUID) (bool, error)
	// FollowerCount is how many accounts follow followed.
	FollowerCount(ctx context.Context, followed uuid.UUID) (int, error)
	// Follows lists the accounts follower follows, newest follow first.
	Follows(ctx context.Context, follower uuid.UUID, p page.Request) (page.Page[FollowView], error)
	// Followers lists the accounts that follow followed, newest follow
	// first.
	Followers(ctx context.Context, followed uuid.UUID, p page.Request) (page.Page[Follower], error)
	// FindByPhone returns the account holding the phone number q.
	FindByPhone(ctx context.Context, q string) (uuid.UUID, bool, error)
	// Identities lists an account's sign-in methods.
	Identities(ctx context.Context, accountID uuid.UUID) ([]Identity, error)
	// SendEmailCode mails a six-digit code to an address: a sign-in code,
	// or with AccountID one that adds the address to that account.
	SendEmailCode(ctx context.Context, in SendEmailCodeInput) (EmailCodeView, error)

	// The address book: only the owner reads or changes an address; any
	// other caller gets domain.ErrAddressNotFound.

	// Addresses lists owner's addresses, the default first, then the
	// newest.
	Addresses(ctx context.Context, owner uuid.UUID) ([]AddressView, error)
	// Address reads one of owner's addresses; module deal copies it into
	// an order.
	Address(ctx context.Context, owner, id uuid.UUID) (AddressView, error)
	// CreateAddress adds an address under the request's key; the first
	// becomes the default. A key owner used already returns the address
	// that request added. An account at domain.MaxAddresses is
	// domain.ErrAddressLimit.
	CreateAddress(ctx context.Context, owner, key uuid.UUID, d domain.AddressDraft) (AddressView, error)
	// UpdateAddress replaces an address with d.
	UpdateAddress(ctx context.Context, owner, id uuid.UUID, d domain.AddressDraft) (AddressView, error)
	SetDefaultAddress(ctx context.Context, owner, id uuid.UUID) error
	// DeleteAddress removes an address; when it was the default, the newest
	// remaining one becomes the default.
	DeleteAddress(ctx context.Context, owner, id uuid.UUID) error
}

// Sessions is the part of Service that signs people in and keeps their
// sessions.
type Sessions interface {
	// SignIn checks a phone token, finds or creates the account and starts
	// a session.
	SignIn(ctx context.Context, in SignInInput) (SignedIn, error)
	// Refresh rotates a refresh token.
	Refresh(ctx context.Context, refreshToken string) (domain.Tokens, error)
	// DeleteCurrentSession revokes the caller's session.
	DeleteCurrentSession(ctx context.Context, p authn.Principal) error
	// DeleteAllSessions revokes every session of the caller's account.
	DeleteAllSessions(ctx context.Context, p authn.Principal) error
	// DeleteSession signs one of the caller's devices out.
	DeleteSession(ctx context.Context, p authn.Principal, id uuid.UUID) error
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
	Follows     repository.Follows
	Addresses   repository.Addresses
	Credentials repository.Credentials
	Sessions    repository.Sessions
	Signer      domain.Signer
	Phones      PhoneVerifier
	Challenges  repository.Challenges
	Quotas      repository.Quotas
	Codes       domain.Codes
	Email       email.Sender
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
	follows     repository.Follows
	addresses   repository.Addresses
	credentials repository.Credentials
	sessions    repository.Sessions
	signer      domain.Signer
	phones      PhoneVerifier
	challenges  repository.Challenges
	quotas      repository.Quotas
	codes       domain.Codes
	email       email.Sender
	media       mediasvc.Service
	prefix      string
	now         func() time.Time
	log         *slog.Logger

	signIn        restatex.Caller[PhoneSignIn, SignInResult]
	emailCodeSend restatex.Caller[EmailCodeInput, EmailCodeResult]
}

var _ Service = (*Account)(nil)

// New builds Account from d.
func New(d Deps) *Account {
	return &Account{
		accounts: d.Accounts, follows: d.Follows, addresses: d.Addresses, credentials: d.Credentials, sessions: d.Sessions, signer: d.Signer, phones: d.Phones,
		challenges: d.Challenges, quotas: d.Quotas, codes: d.Codes, email: d.Email,
		media: d.Media, prefix: d.Prefix, now: d.Now, log: d.Log,
		signIn: restatex.Caller[PhoneSignIn, SignInResult]{Ingress: d.Ingress, Service: d.Prefix + signInService, Handler: "Run"},
		emailCodeSend: restatex.Caller[EmailCodeInput, EmailCodeResult]{
			Ingress: d.Ingress, Service: d.Prefix + emailCodeSendService, Handler: "Run",
		},
	}
}

// Durable returns the Restate services Account calls, for Mount to bind.
func (s *Account) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(SignIn{Prefix: s.prefix, Accounts: s.accounts, Credentials: s.credentials, Now: s.now}),
		restate.Reflect(EmailCodeSend{
			Prefix: s.prefix, Quotas: s.quotas, Challenges: s.challenges, Codes: s.codes, Email: s.email, Now: s.now,
		}),
	}
}
