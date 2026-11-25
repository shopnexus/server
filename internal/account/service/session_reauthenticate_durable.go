package service

import (
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// ReauthenticateRun is an address a token proved outside Restate, or a
// guess at a sign-in code, that must name an identity of account
// AccountID.
type ReauthenticateRun struct {
	AccountID uuid.UUID        `json:"account_id"`
	Address   *contact.Address `json:"address,omitempty"`
	Guess     *CodeGuess       `json:"guess,omitempty"`
	Key       uuid.UUID        `json:"key,omitzero"`
}

// ReauthenticateResult carries a refusal as data, or the time the proof was
// checked, which becomes the session's auth_time.
type ReauthenticateResult struct {
	At      time.Time     `json:"at"`
	Refused *apperr.Error `json:"refused,omitempty"`
}

func (r ReauthenticateResult) Refusal() *apperr.Error { return r.Refused }

// Reauthenticate checks that a proof names an identity of the caller's
// account. It writes only the counted guess of a code: an identity of
// another account or of none is wrong_account, and no account or identity
// is created or changed.
type Reauthenticate struct {
	Prefix      string
	Credentials repository.Credentials
	Challenges  repository.Challenges
	Now         func() time.Time
}

// reauthenticateService is the Reauthenticate service's name without the
// prefix.
const reauthenticateService = "Reauthenticate"

func (s Reauthenticate) ServiceName() string { return s.Prefix + reauthenticateService }

func (s Reauthenticate) Run(ctx restate.Context, in ReauthenticateRun) (ReauthenticateResult, error) {
	at, err := restatex.Now(ctx, s.Now)
	if err != nil {
		return ReauthenticateResult{}, err
	}
	var addr contact.Address
	if in.Guess != nil {
		// A code from POST /v1/auth/email-codes, the one a sign-in takes.
		proved, refused, judgeErr := judgeGuess(ctx, s.Challenges, *in.Guess, domain.PurposeSignIn, nil, in.Key, at)
		if judgeErr != nil || refused != nil {
			return ReauthenticateResult{Refused: refused}, judgeErr
		}
		// A retry of the request proves at the first time, so a code used
		// once cannot renew the sign-in again.
		addr, at = proved.Address, proved.At
	} else {
		addr = *in.Address
	}
	f, err := findIdentity(ctx, s.Credentials, addr)
	if err != nil {
		return ReauthenticateResult{}, err
	}
	if !f.Found || f.Ref.AccountID != in.AccountID {
		return ReauthenticateResult{Refused: domain.ErrWrongAccount}, nil
	}
	return ReauthenticateResult{At: at}, nil
}
