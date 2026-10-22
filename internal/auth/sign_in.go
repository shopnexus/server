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
	"github.com/shopnexus/server2/internal/auth/otp"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

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

// SignInHandler serves POST /v1/auth/otp/verify.
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
		httpx.Respond(w, http.StatusOK, res)
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
	if err != nil {
		return SignInResponse{}, err
	}
	if res.Refused != nil {
		return SignInResponse{}, res.Refused
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
