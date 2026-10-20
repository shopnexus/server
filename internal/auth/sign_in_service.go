package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/httpx"
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
	AccountID uuid.UUID `json:"account_id"`
	IsNew     bool      `json:"is_new"`
	SessionID uuid.UUID `json:"session_id"`
	Refusal   *Refusal  `json:"refusal,omitempty"`
}

// SignIn verifies a code and finds or creates the account. Creating one is a
// saga: Account, then Credentials, and the account is deleted when the
// credentials cannot be created.
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

type holder struct {
	AccountID  uuid.UUID `json:"account_id"`
	IdentityID uuid.UUID `json:"identity_id"`
	Found      bool      `json:"found"`
}

func (s SignIn) Run(ctx restate.Context, in SignInInput) (SignInResult, error) {
	at, err := clock(ctx)
	if err != nil {
		return SignInResult{}, err
	}
	verifyWrite := newID(ctx)
	v, err := restate.Run(ctx, func(rc restate.RunContext) (verified, error) {
		dest, err := VerifyChallenge(rc, s.Challenges, in.ChallengeID, in.CodeHash, at, verifyWrite)
		r, err := refuse(err)
		return verified{Kind: dest.Kind(), Value: dest.Value(), Refusal: r}, err
	})
	if err != nil || v.Refusal != nil {
		return SignInResult{Refusal: v.Refusal}, err
	}
	dest := contact.Restore(v.Kind, v.Value)

	h, err := restate.Run(ctx, func(rc restate.RunContext) (holder, error) {
		acc, ident, err := s.Credentials.FindAccount(rc, credentials.KindOf(dest), dest.Value())
		if errors.Is(err, credentials.ErrNotFound) {
			return holder{}, nil
		}
		return holder{AccountID: acc, IdentityID: ident, Found: err == nil}, err
	})
	if err != nil {
		return SignInResult{}, err
	}
	sessionID := newID(ctx)

	if h.Found {
		markWrite := newID(ctx)
		refusal, err := restate.Run(ctx, func(rc restate.RunContext) (*Refusal, error) {
			return refuse(SignInExisting(rc, s.Accounts, s.Credentials, h.AccountID, h.IdentityID, at, markWrite))
		})
		if err != nil || refusal != nil {
			return SignInResult{Refusal: refusal}, err
		}
		return SignInResult{AccountID: h.AccountID, SessionID: sessionID}, nil
	}

	accountID, identityID := newID(ctx), newID(ctx)
	if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Create(rc, accountID, "") }); err != nil {
		return SignInResult{}, err
	}
	refusal, err := restate.Run(ctx, func(rc restate.RunContext) (*Refusal, error) {
		c := credentials.New(accountID, credentials.NewIdentity(identityID, dest, at), at)
		return refuse(s.Credentials.Create(rc, c))
	})
	if err != nil {
		return SignInResult{}, err
	}
	if refusal != nil {
		// Another sign-up took the identity between the lookup and the
		// insert: undo the account and let the client try again.
		if err := restate.RunVoid(ctx, func(rc restate.RunContext) error { return s.Accounts.Delete(rc, accountID) }); err != nil {
			return SignInResult{}, err
		}
		r, _ := refuse(ErrTryAgain)
		return SignInResult{Refusal: r}, nil
	}
	return SignInResult{AccountID: accountID, IsNew: true, SessionID: sessionID}, nil
}

type SignInRequest struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Code        string    `json:"code"`
	DeviceName  string    `json:"device_name"`
}

type SignedInAccount struct {
	ID    uuid.UUID `json:"id"`
	IsNew bool      `json:"is_new"`
}

type SignInResponse struct {
	session.Tokens
	Account SignedInAccount `json:"account"`
}

// POST /v1/auth/otp/verify
func SignInHandler(rs Restate, challenges otp.ChallengeStore, codes otp.Codes, sessions session.Store, signer session.Signer, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, err := signIn(r, rs, challenges, codes, sessions, signer, now)
		result := "success"
		if err != nil {
			result = "failure"
		}
		logins.Add(r.Context(), 1, metric.WithAttributes(attribute.String("method", "otp"), attribute.String("result", result)))
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, r, http.StatusOK, res)
	})
}

func signIn(r *http.Request, rs Restate, challenges otp.ChallengeStore, codes otp.Codes, sessions session.Store, signer session.Signer, now func() time.Time) (SignInResponse, error) {
	in, err := httpx.Decode[SignInRequest](r)
	if err != nil {
		return SignInResponse{}, err
	}
	c, err := challenges.Get(r.Context(), in.ChallengeID)
	if errors.Is(err, otp.ErrNotFound) {
		return SignInResponse{}, otp.ErrCodeExpired
	}
	if err != nil {
		return SignInResponse{}, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), restateTimeout)
	defer cancel()
	res, err := rs.SignIn().Request(ctx, SignInInput{ChallengeID: in.ChallengeID, CodeHash: codes.Hash(c.Destination(), in.Code)},
		restate.WithIdempotencyKey(r.Header.Get("Idempotency-Key")))
	if err == nil {
		err = res.Refusal.Err()
	}
	if err != nil {
		return SignInResponse{}, err
	}
	tokens, err := IssueSession(r.Context(), sessions, signer, res.SessionID, res.AccountID, in.DeviceName, now())
	if err != nil {
		return SignInResponse{}, err
	}
	return SignInResponse{Tokens: tokens, Account: SignedInAccount{ID: res.AccountID, IsNew: res.IsNew}}, nil
}
