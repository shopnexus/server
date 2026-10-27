// Package service holds module auth's use cases: sending and verifying OTP
// codes, sessions and their tokens, and the identities other modules read.
// Sending a code and verifying one run on Restate, in the *_durable.go
// files; Auth reaches them through restatex.Caller.
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
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/email"
	"github.com/shopnexus/server2/internal/provider/sms"
)

// Service is module auth's contract.
type Service interface {
	// SendOTP sends a sign-in code and names its challenge.
	SendOTP(ctx context.Context, in SendOTPInput) (OTPSent, error)
	// SignIn verifies a code, finds or creates the account and starts a
	// session.
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

// Deps is what New builds Auth from.
type Deps struct {
	Challenges  repository.Challenges
	Quotas      repository.Quotas
	Credentials repository.Credentials
	Sessions    repository.Sessions
	Codes       domain.Codes
	Signer      domain.Signer
	Accounts    accountsvc.Service
	SMS         sms.Sender
	Email       email.Sender
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
	Log     *slog.Logger
}

// Auth implements Service in this process.
type Auth struct {
	challenges  repository.Challenges
	quotas      repository.Quotas
	credentials repository.Credentials
	sessions    repository.Sessions
	codes       domain.Codes
	signer      domain.Signer
	accounts    accountsvc.Service
	sms         sms.Sender
	email       email.Sender
	prefix      string
	now         func() time.Time
	log         *slog.Logger
	otp         restatex.Caller[OTPSendInput, OTPSendResult]
	signIn      restatex.Caller[VerifyInput, VerifyResult]
}

var _ Service = (*Auth)(nil)

func New(d Deps) *Auth {
	return &Auth{
		challenges: d.Challenges, quotas: d.Quotas, credentials: d.Credentials, sessions: d.Sessions,
		codes: d.Codes, signer: d.Signer, accounts: d.Accounts, sms: d.SMS, email: d.Email,
		prefix: d.Prefix, now: d.Now, log: d.Log,
		otp:    restatex.Caller[OTPSendInput, OTPSendResult]{Ingress: d.Ingress, Service: d.Prefix + otpService, Handler: "Send"},
		signIn: restatex.Caller[VerifyInput, VerifyResult]{Ingress: d.Ingress, Service: d.Prefix + signInService, Handler: "Run"},
	}
}

// Durable returns the Restate services Auth calls, for Mount to bind.
func (a *Auth) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(OTP{
			Prefix: a.prefix, Quotas: a.quotas, Challenges: a.challenges, Codes: a.codes, SMS: a.sms, Email: a.email,
			Now: a.now,
		}),
		restate.Reflect(SignIn{
			Prefix: a.prefix, Challenges: a.challenges, Credentials: a.credentials, Accounts: a.accounts, Now: a.now,
		}),
	}
}
