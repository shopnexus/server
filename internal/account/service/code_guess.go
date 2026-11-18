package service

import (
	"context"
	"crypto/hmac"
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

// CodeGuess is a guess at an email code as Restate sees it: the challenge
// and the code's HMAC, never the code, because Restate keeps inputs in its
// journal.
type CodeGuess struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	CodeHash    []byte    `json:"code_hash"`
}

// loadedChallenge is the challenge as the judging step read it.
type loadedChallenge struct {
	Challenge domain.Challenge `json:"challenge"`
	Found     bool             `json:"found"`
	Newest    bool             `json:"newest"`
}

// judgeGuess judges g on the challenge as one step read it and saves the
// counted guess in the next: a refused guess changes nothing and is not
// saved, and a guess that loses a race gets try_again, right or wrong, and
// is not counted. A challenge that is unknown, replaced by a newer one to
// its address, of another purpose or of another account than owner is
// code_expired. SignIn and AddIdentity call it; it returns the address the
// right code proves.
func judgeGuess(ctx restate.Context, challenges repository.Challenges, g CodeGuess, purpose domain.Purpose,
	owner *uuid.UUID, at time.Time,
) (contact.Address, *apperr.Error, error) {
	l, err := restate.Run(ctx, func(rc restate.RunContext) (loadedChallenge, error) {
		c, stepErr := challenges.Get(rc, g.ChallengeID)
		if errors.Is(stepErr, repository.ErrNotFound) {
			return loadedChallenge{}, nil
		}
		if stepErr != nil {
			return loadedChallenge{}, fmt.Errorf("get challenge: %w", stepErr)
		}
		newest, stepErr := challenges.IsNewest(rc, c)
		if stepErr != nil {
			return loadedChallenge{}, fmt.Errorf("check newest challenge: %w", stepErr)
		}
		return loadedChallenge{Challenge: c, Found: true, Newest: newest}, nil
	})
	if err != nil {
		return contact.Address{}, nil, fmt.Errorf("load challenge: %w", err)
	}
	c := l.Challenge
	sameOwner := (owner == nil && c.AccountID() == nil) || (owner != nil && c.AccountID() != nil && *owner == *c.AccountID())
	if !l.Found || !l.Newest || c.Purpose() != purpose || !sameOwner {
		return contact.Address{}, domain.ErrCodeExpired, nil
	}
	if closed := c.Open(at); closed != nil {
		return contact.Address{}, apperr.From(closed), nil
	}
	verdict := c.Verify(g.CodeHash, at)
	w := restatex.NewID(ctx, at)
	lost, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		c := c // Save sets the write ID on its copy; c outside stays as journaled
		return restatex.Outcome(challenges.Save(rc, &c, w), repository.ErrConflict)
	})
	if err != nil {
		return contact.Address{}, nil, fmt.Errorf("save guess: %w", err)
	}
	if lost {
		return contact.Address{}, apperr.ErrTryAgain, nil
	}
	if verdict != nil {
		return contact.Address{}, apperr.From(verdict), nil
	}
	return c.Destination(), nil, nil
}

// verifyToken checks a phone or Google token outside Restate and returns the
// address it proves. Account.SignIn, Account.AddIdentity and
// Account.Reauthenticate call it for a proof without an email code. A token
// the verifier refuses keeps its code and takes kind refused. SignIn passes
// apperr.KindUnauthenticated, because there the token signs the request in.
// The other two pass apperr.KindInvalid: a bearer signs those requests in,
// and a client takes a 401 on a bearer call for an expired or revoked
// bearer.
func (s *Account) verifyToken(ctx context.Context, p Proof, refused apperr.Kind) (contact.Address, error) {
	verifier, token, name := s.phones, p.PhoneToken, "phone"
	if p.GoogleToken != "" {
		verifier, token, name = s.google, p.GoogleToken, "google"
	}
	a, err := verifier.Verify(ctx, token)
	if e, ok := errors.AsType[*apperr.Error](err); ok && e.Kind == apperr.KindUnauthenticated {
		err = e.WithKind(refused)
	}
	if err != nil {
		return contact.Address{}, fmt.Errorf("verify %s token: %w", name, err)
	}
	return a, nil
}

// guessFor hashes code against its challenge outside Restate. It also
// reports whether the code matches the challenge, which a caller checks on
// the answer to a repeated Idempotency-Key: Restate returns the first run's
// result whatever this request carries. An unknown challenge is
// code_expired.
func (s *Account) guessFor(ctx context.Context, code EmailCode) (CodeGuess, bool, error) {
	c, err := s.challenges.Get(ctx, code.ChallengeID)
	if errors.Is(err, repository.ErrNotFound) {
		return CodeGuess{}, false, domain.ErrCodeExpired
	}
	if err != nil {
		return CodeGuess{}, false, fmt.Errorf("get challenge: %w", err)
	}
	hash := s.codes.Hash(c.Destination(), code.Code)
	return CodeGuess{ChallengeID: c.ID(), CodeHash: hash}, hmac.Equal(hash, c.CodeHash()), nil
}
