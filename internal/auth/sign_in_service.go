package auth

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

// SignInInput carries the code's HMAC, never the code, because Restate keeps
// inputs in its journal.
type SignInInput struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	CodeHash    []byte    `json:"code_hash"`
}

// SignInResult names the account and the session the handler issues tokens
// for. Tokens never enter Restate. A refusal travels as data, as in
// OTPSendResult.
//
//exhaustruct:optional
type SignInResult struct {
	ChallengeID uuid.UUID     `json:"challenge_id"`
	AccountID   uuid.UUID     `json:"account_id"`
	IsNew       bool          `json:"is_new"`
	SessionID   uuid.UUID     `json:"session_id"`
	Refused     *apperr.Error `json:"refused,omitempty"`
}

// SignIn verifies a code and finds or creates the account. Creating one is a
// saga: Account, then Credentials, and the account is deleted when another
// sign-up took the identity in between. Any other failure is retried.
type SignIn struct {
	Prefix      string
	Challenges  otp.ChallengeStore
	Credentials credentials.Store
	Accounts    AccountPort
	Now         func() time.Time
}

// signInService is the SignIn service's name without the prefix.
const signInService = "SignIn"

func (s SignIn) ServiceName() string { return s.Prefix + signInService }

// loadedChallenge is the challenge as the verify step read it.
type loadedChallenge struct {
	Challenge otp.Challenge `json:"challenge"`
	Found     bool          `json:"found"`
	Newest    bool          `json:"newest"`
}

// foundIdentity is FindIdentity's answer as the journal keeps it.
type foundIdentity struct {
	Ref   credentials.IdentityRef `json:"ref"`
	Found bool                    `json:"found"`
}

func (s SignIn) Run(ctx restate.Context, in SignInInput) (SignInResult, error) {
	at, err := clock(ctx, s.Now)
	if err != nil {
		return SignInResult{}, err
	}

	// Judge the code on the challenge as one step read it, and save the
	// counted guess in the next step. A refused guess changes nothing and is
	// not saved. A guess that loses a race gets try_again whether it was
	// right or wrong, and is not counted.
	l, err := restate.Run(ctx, func(rc restate.RunContext) (loadedChallenge, error) {
		c, stepErr := s.Challenges.Get(rc, in.ChallengeID)
		if errors.Is(stepErr, otp.ErrNotFound) {
			return loadedChallenge{}, nil
		}
		if stepErr != nil {
			return loadedChallenge{}, fmt.Errorf("get challenge: %w", stepErr)
		}
		newest, stepErr := s.Challenges.IsNewest(rc, c)
		if stepErr != nil {
			return loadedChallenge{}, fmt.Errorf("check newest challenge: %w", stepErr)
		}
		return loadedChallenge{Challenge: c, Found: true, Newest: newest}, nil
	})
	if err != nil {
		return SignInResult{}, fmt.Errorf("load challenge: %w", err)
	}
	c := l.Challenge
	if !l.Found || !l.Newest || c.Purpose() != otp.PurposeLogin {
		return SignInResult{Refused: otp.ErrCodeExpired}, nil
	}
	if err = c.Open(at); err != nil {
		return SignInResult{Refused: apperr.From(err)}, nil
	}
	verdict := c.Verify(in.CodeHash, at)
	verifyWrite := newID(ctx, at)
	lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		c := c // Save sets the write ID on its copy; c outside stays as journaled
		stepErr := s.Challenges.Save(rc, &c, verifyWrite)
		return errors.Is(stepErr, otp.ErrConflict), ignore(stepErr, otp.ErrConflict)
	})
	if err != nil {
		return SignInResult{}, fmt.Errorf("save guess: %w", err)
	}
	if lost {
		return SignInResult{Refused: apperr.ErrTryAgain}, nil
	}
	if verdict != nil {
		return SignInResult{Refused: apperr.From(verdict)}, nil
	}
	dest := c.Destination()

	f, err := restate.Run(ctx, func(rc restate.RunContext) (foundIdentity, error) {
		ref, stepErr := s.Credentials.FindIdentity(rc, credentials.KindOf(dest), dest.Value())
		if errors.Is(stepErr, credentials.ErrNotFound) {
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
	sessionID := newID(ctx, at)

	if f.Found {
		// Only a person who just proved they hold the identity gets here, so
		// account_suspended is safe to reveal.
		st, err := restate.Run(ctx, func(rc restate.RunContext) (account.Status, error) {
			return s.Accounts.Status(rc, f.Ref.AccountID)
		})
		if err != nil {
			return SignInResult{}, fmt.Errorf("get account status: %w", err)
		}
		if st == account.StatusSuspended {
			return SignInResult{Refused: account.ErrSuspended}, nil
		}
		// last_used_at is a hint for the dormant check. A lost race or an
		// identity removed meanwhile costs nothing, so the step ignores both.
		markWrite := newID(ctx, at)
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error {
			c, stepErr := s.Credentials.Get(rc, f.Ref.AccountID)
			if stepErr == nil {
				stepErr = c.MarkUsed(f.Ref.IdentityID, at)
			}
			if stepErr == nil {
				stepErr = ignore(s.Credentials.Save(rc, &c, markWrite), credentials.ErrConflict)
			}
			return ignore(stepErr, credentials.ErrNotFound)
		}); err != nil {
			return SignInResult{}, fmt.Errorf("mark identity used: %w", err)
		}
		return SignInResult{ChallengeID: in.ChallengeID, AccountID: f.Ref.AccountID, SessionID: sessionID}, nil
	}

	accountID, identityID := newID(ctx, at), newID(ctx, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Create(rc, accountID, "") }); err != nil {
		return SignInResult{}, fmt.Errorf("create account: %w", err)
	}
	taken, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		c := credentials.New(accountID, credentials.NewIdentity(identityID, dest, at), at)
		stepErr := s.Credentials.Create(rc, c)
		return errors.Is(stepErr, credentials.ErrTaken), ignore(stepErr, credentials.ErrTaken)
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
	return SignInResult{ChallengeID: in.ChallengeID, AccountID: accountID, IsNew: true, SessionID: sessionID}, nil
}
