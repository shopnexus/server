package service

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// ProvenSignIn is an address a token proved outside Restate, or a guess at
// an email code for the flow to judge. Neither the token nor the code
// enters Restate, whose journal the admin API can read. Key is the client's
// Idempotency-Key.
type ProvenSignIn struct {
	Address *contact.Address `json:"address,omitempty"`
	Guess   *CodeGuess       `json:"guess,omitempty"`
	Key     uuid.UUID        `json:"key,omitzero"`
}

// SignInResult names the account Account.SignIn starts a session of. Tokens
// never enter Restate. A refusal travels as data.
type SignInResult struct {
	AccountID uuid.UUID     `json:"account_id"`
	IsNew     bool          `json:"is_new"`
	Refused   *apperr.Error `json:"refused,omitempty"`
}

func (r SignInResult) Refusal() *apperr.Error { return r.Refused }

// SignIn finds or creates the account of a proven phone number or email
// address. A new account and
// its credentials are written in one transaction, so a sign-up that loses the
// number to another one in between writes nothing. Any other failure is
// retried.
type SignIn struct {
	Prefix      string
	Accounts    repository.Accounts
	Credentials repository.Credentials
	Challenges  repository.Challenges
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

// accountStatus is the found account's status, and whether its sign-up
// carried this request's key.
type accountStatus struct {
	Status   domain.Status `json:"status"`
	SignedUp bool          `json:"signed_up"`
}

// findIdentity looks up the identity of addr in a step. SignIn and
// Reauthenticate call it.
func findIdentity(ctx restate.Context, credentials repository.Credentials, addr contact.Address) (foundIdentity, error) {
	f, err := restate.Run(ctx, func(rc restate.RunContext) (foundIdentity, error) {
		ref, stepErr := credentials.FindIdentity(rc, domain.Kind(addr.Kind()), addr.Value())
		if errors.Is(stepErr, repository.ErrNotFound) {
			return foundIdentity{}, nil
		}
		if stepErr != nil {
			return foundIdentity{}, fmt.Errorf("find identity: %w", stepErr)
		}
		return foundIdentity{Ref: ref, Found: true}, nil
	})
	if err != nil {
		return foundIdentity{}, fmt.Errorf("look up identity: %w", err)
	}
	return f, nil
}

func (s SignIn) Run(ctx restate.Context, in ProvenSignIn) (SignInResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return SignInResult{}, err
	}
	var addr contact.Address
	if in.Guess != nil {
		proved, refused, judgeErr := judgeGuess(ctx, s.Challenges, *in.Guess, domain.PurposeSignIn, nil, in.Key, at)
		if judgeErr != nil || refused != nil {
			return SignInResult{Refused: refused}, judgeErr
		}
		addr = proved.Address
	} else {
		addr = *in.Address
	}

	f, err := findIdentity(ctx, s.Credentials, addr)
	if err != nil {
		return SignInResult{}, err
	}

	if f.Found {
		// Only a person who just proved they hold the identity gets here, so
		// account_suspended is safe to reveal.
		// A retry of the sign-up that made the account answers is_new as
		// the first request did.
		st, err := restate.Run(ctx, func(rc restate.RunContext) (accountStatus, error) {
			a, stepErr := s.Accounts.Get(rc, f.Ref.AccountID)
			if stepErr != nil {
				return accountStatus{}, fmt.Errorf("get account status: %w", stepErr)
			}
			signedUp, stepErr := s.Credentials.SignedUpWith(rc, f.Ref.AccountID, in.Key)
			if stepErr != nil {
				return accountStatus{}, fmt.Errorf("check the sign-up key: %w", stepErr)
			}
			return accountStatus{Status: a.Status(at), SignedUp: signedUp}, nil
		})
		if err != nil {
			return SignInResult{}, fmt.Errorf("get account status: %w", err)
		}
		switch st.Status {
		case domain.StatusSuspended:
			return SignInResult{Refused: domain.ErrSuspended}, nil
		case domain.StatusDeleted:
			// The identity was found before Erase deleted it with the
			// account; the person's retry finds no identity and signs up.
			return SignInResult{Refused: apperr.ErrTryAgain}, nil
		case domain.StatusActive:
		default: // the CHECK on accounts.status allows no other status
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
		return SignInResult{AccountID: f.Ref.AccountID, IsNew: st.SignedUp}, nil
	}

	accountID, identityID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	a, err := domain.New(accountID, "", at)
	if err != nil {
		return SignInResult{}, fmt.Errorf("new account: %w", err)
	}
	c := domain.NewCredentials(accountID, domain.NewIdentity(identityID, addr, at), at)
	taken, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		return restatex.Outcome(s.Credentials.SignUp(rc, a, c, in.Key), domain.ErrIdentityTaken)
	})
	if err != nil {
		return SignInResult{}, fmt.Errorf("create account: %w", err)
	}
	if taken {
		// Another sign-up took the identity between the lookup and the
		// insert, and the transaction wrote nothing: the client tries again.
		return SignInResult{Refused: apperr.ErrTryAgain}, nil
	}
	return SignInResult{AccountID: accountID, IsNew: true}, nil
}
