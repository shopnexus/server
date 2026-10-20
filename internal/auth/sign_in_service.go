package auth

import (
	"errors"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// SignInInput carries the code's HMAC, never the code, because Restate keeps
// inputs in its journal.
type SignInInput struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	CodeHash    []byte    `json:"code_hash"`
}

// SignInResult names the account and the session the handler issues tokens
// for. Tokens never enter Restate.
type SignInResult struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	AccountID   uuid.UUID `json:"account_id"`
	IsNew       bool      `json:"is_new"`
	SessionID   uuid.UUID `json:"session_id"`
	Refusal     *Refusal  `json:"refusal,omitempty"`
}

// SignIn verifies a code and finds or creates the account. Creating one is a
// saga: Account, then Credentials, and the account is deleted when another
// sign-up took the identity in between. Any other failure is retried.
type SignIn struct {
	Prefix      string
	Challenges  otp.ChallengeStore
	Credentials credentials.Store
	Accounts    AccountPort
}

func (s SignIn) ServiceName() string { return s.Prefix + "SignIn" }

type verified struct {
	Kind    contact.Kind `json:"kind"`
	Value   string       `json:"value"`
	Refusal *Refusal     `json:"refusal,omitempty"`
}

// found is FindIdentity's answer as the journal keeps it.
type found struct {
	Ref   credentials.IdentityRef `json:"ref"`
	Found bool                    `json:"found"`
}

func (s SignIn) Run(ctx restate.Context, in SignInInput) (SignInResult, error) {
	at, err := clock(ctx)
	if err != nil {
		return SignInResult{}, err
	}
	verifyWrite := newID(ctx, at)
	v, err := restate.Run(ctx, func(rc restate.RunContext) (verified, error) {
		dest, err := VerifyChallenge(rc, s.Challenges, in.ChallengeID, in.CodeHash, at, verifyWrite)
		r, err := refuse(err)
		return verified{Kind: dest.Kind(), Value: dest.Value(), Refusal: r}, err
	})
	if err != nil || v.Refusal != nil {
		return SignInResult{Refusal: v.Refusal}, err
	}
	dest := contact.Restore(v.Kind, v.Value)

	f, err := restate.Run(ctx, func(rc restate.RunContext) (found, error) {
		ref, err := s.Credentials.FindIdentity(rc, credentials.KindOf(dest), dest.Value())
		if errors.Is(err, credentials.ErrNotFound) {
			return found{}, nil
		}
		return found{Ref: ref, Found: err == nil}, err
	})
	if err != nil {
		return SignInResult{}, err
	}
	sessionID := newID(ctx, at)

	if f.Found {
		markWrite := newID(ctx, at)
		refusal, err := restate.Run(ctx, func(rc restate.RunContext) (*Refusal, error) {
			return refuse(SignInExisting(rc, s.Accounts, s.Credentials, f.Ref, at, markWrite))
		})
		if err != nil || refusal != nil {
			return SignInResult{Refusal: refusal}, err
		}
		return SignInResult{ChallengeID: in.ChallengeID, AccountID: f.Ref.AccountID, SessionID: sessionID}, nil
	}

	accountID, identityID := newID(ctx, at), newID(ctx, at)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Create(rc, accountID, "") }); err != nil {
		return SignInResult{}, err
	}
	taken, err := restate.Run(ctx, func(rc restate.RunContext) (bool, error) {
		c := credentials.New(accountID, credentials.NewIdentity(identityID, dest, at), at)
		err := s.Credentials.Create(rc, c)
		if errors.Is(err, credentials.ErrTaken) {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		return SignInResult{}, err
	}
	if taken {
		// Another sign-up took the identity between the lookup and the
		// insert: undo the account and let the client try again.
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Delete(rc, accountID) }); err != nil {
			return SignInResult{}, err
		}
		return SignInResult{Refusal: refusalOf(ErrTryAgain)}, nil
	}
	return SignInResult{ChallengeID: in.ChallengeID, AccountID: accountID, IsNew: true, SessionID: sessionID}, nil
}
