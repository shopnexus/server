package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// PhoneSignIn is a number Firebase proved. The token that proved it stays
// out of Restate, whose journal the admin API can read.
type PhoneSignIn struct {
	Phone contact.Address `json:"phone"`
}

// SignInResult names the account and the session Auth.SignIn issues tokens
// for. Tokens never enter Restate. A refusal travels as data.
type SignInResult struct {
	AccountID uuid.UUID     `json:"account_id"`
	IsNew     bool          `json:"is_new"`
	SessionID uuid.UUID     `json:"session_id"`
	Refused   *apperr.Error `json:"refused,omitempty"`
}

func (r SignInResult) Refusal() *apperr.Error { return r.Refused }

// SignIn finds or creates the account of a proven number. Creating one is a
// saga: Account, then Credentials, and the account is deleted when another
// sign-up took the number in between. Any other failure is retried.
type SignIn struct {
	Prefix      string
	Credentials repository.Credentials
	Accounts    accountsvc.Service
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
		st, err := restate.Run(ctx, func(rc restate.RunContext) (accountdomain.Status, error) {
			return s.Accounts.Status(rc, f.Ref.AccountID)
		})
		if err != nil {
			return SignInResult{}, fmt.Errorf("get account status: %w", err)
		}
		if st == accountdomain.StatusSuspended {
			return SignInResult{Refused: accountdomain.ErrSuspended}, nil
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
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Create(rc, accountID, "") }); err != nil {
		return SignInResult{}, fmt.Errorf("create account: %w", err)
	}
	taken, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		c := domain.NewCredentials(accountID, domain.NewIdentity(identityID, in.Phone, at), at)
		stepErr := s.Credentials.Create(rc, c)
		return restatex.Outcome(stepErr, domain.ErrTaken)
	})
	if err != nil {
		return SignInResult{}, fmt.Errorf("create credentials: %w", err)
	}
	if taken {
		// Another sign-up took the identity between the lookup and the
		// insert: undo the account and let the client try again.
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Delete(rc, accountID) }); err != nil {
			return SignInResult{}, fmt.Errorf("undo account: %w", err)
		}
		return SignInResult{Refused: apperr.ErrTryAgain}, nil
	}
	return SignInResult{AccountID: accountID, IsNew: true, SessionID: sessionID}, nil
}
