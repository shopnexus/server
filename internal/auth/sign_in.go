package auth

import (
	"context"
	"crypto/hmac"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth/credentials"
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// VerifyChallenge checks a code hash against a login challenge and saves the
// counted guess before it answers, so a wrong code stays counted. A guess
// the challenge refuses changes nothing and is not saved. A write that loses
// a race returns ErrTryAgain whether the code was right or wrong, and that
// guess is not counted.
//
// stepWrite is the step's journaled write ID. A retried step whose save
// already committed finds it on the challenge and reports that guess's
// verdict instead of counting the code again.
func VerifyChallenge(ctx context.Context, challenges otp.ChallengeStore, id uuid.UUID, codeHash []byte, now time.Time, stepWrite uuid.UUID) (contact.Address, error) {
	c, err := challenges.Get(ctx, id)
	if errors.Is(err, otp.ErrNotFound) {
		return contact.Address{}, otp.ErrCodeExpired
	}
	if err != nil {
		return contact.Address{}, err
	}
	if c.Purpose() != otp.PurposeLogin {
		return contact.Address{}, otp.ErrCodeExpired
	}
	if c.WriteID() == stepWrite {
		return c.Destination(), c.Verdict()
	}
	newest, err := challenges.IsNewest(ctx, c)
	if err != nil {
		return contact.Address{}, err
	}
	if !newest {
		return contact.Address{}, otp.ErrCodeExpired
	}
	if err := c.Open(now); err != nil {
		return contact.Address{}, err
	}
	verdict := c.Verify(codeHash, now)
	if err := challenges.Save(ctx, &c, stepWrite); err != nil {
		if errors.Is(err, otp.ErrConflict) {
			return contact.Address{}, ErrTryAgain
		}
		return contact.Address{}, err
	}
	return c.Destination(), verdict
}

// SignInExisting lets an existing account in: it refuses a suspended account
// and records the identity's use. Only a person who just proved they hold the
// identity gets here, so account_suspended is safe to reveal.
func SignInExisting(ctx context.Context, accounts AccountPort, creds credentials.Store, ref credentials.IdentityRef, now time.Time, stepWrite uuid.UUID) error {
	st, err := accounts.Status(ctx, ref.AccountID)
	if err != nil {
		return err
	}
	if st == account.StatusSuspended {
		return account.ErrSuspended
	}
	c, err := creds.Get(ctx, ref.AccountID)
	if err != nil {
		return err
	}
	if err := c.MarkUsed(ref.IdentityID, now); err != nil {
		return err
	}
	// last_used_at is a hint for the dormant check; losing it to a concurrent
	// sign-in of the same account costs nothing.
	if err := creds.Save(ctx, &c, stepWrite); err != nil && !errors.Is(err, credentials.ErrConflict) {
		return err
	}
	return nil
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
	hash := codes.Hash(c.Destination(), in.Code)
	res, err := rs.SignIn().Request(ctx, SignInInput{ChallengeID: in.ChallengeID, CodeHash: hash},
		restate.WithIdempotencyKey(r.Header.Get("Idempotency-Key")))
	if err == nil {
		err = res.Refusal.Err()
	}
	if err != nil {
		return SignInResponse{}, err
	}
	// Restate answers a repeated Idempotency-Key with the first result,
	// whatever this request carries. Only the request that matches it, the
	// same challenge and its code, gets the session.
	if res.ChallengeID != in.ChallengeID || !hmac.Equal(hash, c.CodeHash()) {
		return SignInResponse{}, otp.ErrCodeExpired
	}
	tokens, err := IssueSession(r.Context(), sessions, signer, res.SessionID, res.AccountID, in.DeviceName, now())
	if err != nil {
		return SignInResponse{}, err
	}
	return SignInResponse{Tokens: tokens, Account: SignedInAccount{ID: res.AccountID, IsNew: res.IsNew}}, nil
}
