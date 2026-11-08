package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// PhoneSignIn is a number Firebase proved. The token that proved it stays
// out of Restate, whose journal the admin API can read.
type PhoneSignIn struct {
	Phone contact.Address `json:"phone"`
}

// SignInResult names the account and the session Account.SignIn issues tokens
// for. Tokens never enter Restate. A refusal travels as data.
type SignInResult struct {
	AccountID uuid.UUID     `json:"account_id"`
	IsNew     bool          `json:"is_new"`
	SessionID uuid.UUID     `json:"session_id"`
	Refused   *apperr.Error `json:"refused,omitempty"`
}

func (r SignInResult) Refusal() *apperr.Error { return r.Refused }

// SignIn finds or creates the account of a proven number. A new account and
// its credentials are written in one transaction, so a sign-up that loses the
// number to another one in between writes nothing. Any other failure is
// retried.
type SignIn struct {
	Prefix      string
	Accounts    repository.Accounts
	Credentials repository.Credentials
	Now         func() time.Time
}

// signInService is the SignIn service's name without the prefix.
const signInService = "SignIn"

func (s SignIn) ServiceName() string { return s.Prefix + signInService }

// foundIdentity is FindIdentity's answer as the journal keeps it.
type foundIdentity struct {
	Ref   domain.IdentityRef `json:"ref"`
	Found bool               `json:"found"`
}

func (s SignIn) Run(ctx restate.Context, in PhoneSignIn) (SignInResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return SignInResult{}, err
	}

	f, err := restate.Run(ctx, func(rc restate.RunContext) (foundIdentity, error) {
		ref, stepErr := s.Credentials.FindIdentity(rc, domain.KindPhone, in.Phone.Value())
		if errors.Is(stepErr, repository.ErrNotFound) {
			return foundIdentity{}, nil
		}
		if stepErr != nil {
			return foundIdentity{}, fmt.Errorf("find identity: %w", stepErr)
		}
		return foundIdentity{Ref: ref, Found: true}, nil
	})
	if err != nil {
		return SignInResult{}, fmt.Errorf("look up identity: %w", err)
	}
	sessionID := restatex.NewID(ctx, at)

	if f.Found {
		// Only a person who just proved they hold the identity gets here, so
		// account_suspended is safe to reveal.
		suspended, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
			a, stepErr := s.Accounts.Get(rc, f.Ref.AccountID)
			if stepErr != nil {
				return false, fmt.Errorf("get account: %w", stepErr)
			}
			return a.Suspended(at), nil
		})
		if err != nil {
			return SignInResult{}, fmt.Errorf("get account status: %w", err)
		}
		if suspended {
			return SignInResult{Refused: domain.ErrSuspended}, nil
		}
		// last_used_at is a hint for the dormant check. A lost race or an
		// identity removed meanwhile costs nothing, so the step ignores both.
		markWrite := restatex.NewID(ctx, at)
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
			c, stepErr := s.Credentials.Get(rc, f.Ref.AccountID)
			if stepErr == nil {
				stepErr = c.MarkUsed(f.Ref.IdentityID, at)
			}
			if stepErr == nil {
				stepErr = restatex.Ignore(s.Credentials.Save(rc, &c, markWrite), repository.ErrConflict)
			}
			stepErr = restatex.Ignore(stepErr, domain.ErrUnknownIdentity)
			return restatex.Ignore(stepErr, repository.ErrNotFound)
		}); err != nil {
			return SignInResult{}, fmt.Errorf("mark identity used: %w", err)
		}
		return SignInResult{AccountID: f.Ref.AccountID, SessionID: sessionID}, nil
	}

	accountID, identityID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	a, err := domain.New(accountID, "", at)
	if err != nil {
		return SignInResult{}, fmt.Errorf("new account: %w", err)
	}
	c := domain.NewCredentials(accountID, domain.NewIdentity(identityID, in.Phone, at), at)
	taken, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		return restatex.Outcome(s.Credentials.SignUp(rc, a, c), domain.ErrIdentityTaken)
	})
	if err != nil {
		return SignInResult{}, fmt.Errorf("create account: %w", err)
	}
	if taken {
		// Another sign-up took the identity between the lookup and the
		// insert, and the transaction wrote nothing: the client tries again.
		return SignInResult{Refused: apperr.ErrTryAgain}, nil
	}
	return SignInResult{AccountID: accountID, IsNew: true, SessionID: sessionID}, nil
}
