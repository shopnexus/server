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
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// VerifyInput carries the code's HMAC, never the code, because Restate keeps
// inputs in its journal.
type VerifyInput struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	CodeHash    []byte    `json:"code_hash"`
}

// VerifyResult names the account and the session Auth.SignIn issues tokens
// for. Tokens never enter Restate. A refusal travels as data, as in
// OTPSendResult.
type VerifyResult struct {
	ChallengeID uuid.UUID     `json:"challenge_id"`
	AccountID   uuid.UUID     `json:"account_id"`
	IsNew       bool          `json:"is_new"`
	SessionID   uuid.UUID     `json:"session_id"`
	Refused     *apperr.Error `json:"refused,omitempty"`
}

func (r VerifyResult) Refusal() *apperr.Error { return r.Refused }

// SignIn verifies a code and finds or creates the account. Creating one is a
// saga: Account, then Credentials, and the account is deleted when another
// sign-up took the identity in between. Any other failure is retried.
type SignIn struct {
	Prefix      string
	Challenges  repository.Challenges
	Credentials repository.Credentials
	Accounts    accountsvc.Service
	Now         func() time.Time
}

// signInService is the SignIn service's name without the prefix.
const signInService = "SignIn"

func (s SignIn) ServiceName() string { return s.Prefix + signInService }

// loadedChallenge is the challenge as the verify step read it.
type loadedChallenge struct {
	Challenge domain.Challenge `json:"challenge"`
	Found     bool             `json:"found"`
	Newest    bool             `json:"newest"`
}

// foundIdentity is FindIdentity's answer as the journal keeps it.
type foundIdentity struct {
	Ref   domain.IdentityRef `json:"ref"`
	Found bool               `json:"found"`
}

func (s SignIn) Run(ctx restate.Context, in VerifyInput) (VerifyResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return VerifyResult{}, err
	}

	// Judge the code on the challenge as one step read it, and save the
	// counted guess in the next step. A refused guess changes nothing and is
	// not saved. A guess that loses a race gets try_again whether it was
	// right or wrong, and is not counted.
	l, err := restate.Run(ctx, func(rc restate.RunContext) (loadedChallenge, error) {
		c, stepErr := s.Challenges.Get(rc, in.ChallengeID)
		if errors.Is(stepErr, repository.ErrNotFound) {
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
		return VerifyResult{}, fmt.Errorf("load challenge: %w", err)
	}
	c := l.Challenge
	if !l.Found || !l.Newest || c.Purpose() != domain.PurposeLogin {
		return VerifyResult{Refused: domain.ErrCodeExpired}, nil
	}
	if err = c.Open(at); err != nil {
		return VerifyResult{Refused: apperr.From(err)}, nil
	}
	verdict := c.Verify(in.CodeHash, at)
	verifyWrite := restatex.NewID(ctx, at)
	lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		c := c // Save sets the write ID on its copy; c outside stays as journaled
		stepErr := s.Challenges.Save(rc, &c, verifyWrite)
		return restatex.Outcome(stepErr, repository.ErrConflict)
	})
	if err != nil {
		return VerifyResult{}, fmt.Errorf("save guess: %w", err)
	}
	if lost {
		return VerifyResult{Refused: apperr.ErrTryAgain}, nil
	}
	if verdict != nil {
		return VerifyResult{Refused: apperr.From(verdict)}, nil
	}
	dest := c.Destination()

	f, err := restate.Run(ctx, func(rc restate.RunContext) (foundIdentity, error) {
		ref, stepErr := s.Credentials.FindIdentity(rc, domain.KindOf(dest), dest.Value())
		if errors.Is(stepErr, repository.ErrNotFound) {
			return foundIdentity{}, nil
		}
		if stepErr != nil {
			return foundIdentity{}, fmt.Errorf("find identity: %w", stepErr)
		}
		return foundIdentity{Ref: ref, Found: true}, nil
	})
	if err != nil {
		return VerifyResult{}, fmt.Errorf("look up identity: %w", err)
	}
	sessionID := restatex.NewID(ctx, at)

	if f.Found {
		// Only a person who just proved they hold the identity gets here, so
		// account_suspended is safe to reveal.
		st, err := restate.Run(ctx, func(rc restate.RunContext) (accountdomain.Status, error) {
			return s.Accounts.Status(rc, f.Ref.AccountID)
		})
		if err != nil {
			return VerifyResult{}, fmt.Errorf("get account status: %w", err)
		}
		if st == accountdomain.StatusSuspended {
			return VerifyResult{Refused: accountdomain.ErrSuspended}, nil
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
			return VerifyResult{}, fmt.Errorf("mark identity used: %w", err)
		}
		return VerifyResult{ChallengeID: in.ChallengeID, AccountID: f.Ref.AccountID, SessionID: sessionID}, nil
	}

	accountID, identityID := restatex.NewID(ctx, at), restatex.NewID(ctx, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Create(rc, accountID, "") }); err != nil {
		return VerifyResult{}, fmt.Errorf("create account: %w", err)
	}
	taken, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		c := domain.NewCredentials(accountID, domain.NewIdentity(identityID, dest, at), at)
		stepErr := s.Credentials.Create(rc, c)
		return restatex.Outcome(stepErr, domain.ErrTaken)
	})
	if err != nil {
		return VerifyResult{}, fmt.Errorf("create credentials: %w", err)
	}
	if taken {
		// Another sign-up took the identity between the lookup and the
		// insert: undo the account and let the client try again.
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Delete(rc, accountID) }); err != nil {
			return VerifyResult{}, fmt.Errorf("undo account: %w", err)
		}
		return VerifyResult{Refused: apperr.ErrTryAgain}, nil
	}
	return VerifyResult{ChallengeID: in.ChallengeID, AccountID: accountID, IsNew: true, SessionID: sessionID}, nil
}
