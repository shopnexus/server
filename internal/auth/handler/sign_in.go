package handler

import (
	"net/http"
	"uuid"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type signInRequest struct {
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
	ChallengeID    uuid.UUID `json:"challenge_id"`
	Code           string    `json:"code"`
	DeviceName     string    `json:"device_name"`
}

// SignIn serves POST /v1/auth/otp/verify. Every request counts in
// login_total, a malformed one as a failure.
func (h *Handler) SignIn(w http.ResponseWriter, r *http.Request) {
	result := "failure"
	defer func() {
		logins.Add(r.Context(), 1, metric.WithAttributes(attribute.String("method", "otp"), attribute.String("result", result)))
	}()
	var in signInRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	res, err := h.svc.SignIn(r.Context(), service.SignInInput{
		IdempotencyKey: in.IdempotencyKey, ChallengeID: in.ChallengeID, Code: in.Code, DeviceName: in.DeviceName,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	result = "success"
	httpx.Respond(w, http.StatusOK, res)
}
