package handler

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type signInRequest struct {
	proofBody

	IdempotencyKey string `header:"Idempotency-Key" json:"-"`
	DeviceName     string `json:"device_name"`
}

// SignIn serves POST /v1/auth/sign-in. Every request counts in login_total
// under its proof's method, a malformed one as a phone failure.
func (h *Handler) SignIn(w http.ResponseWriter, r *http.Request) {
	result := "failure"
	var in signInRequest
	defer func() {
		logins.Add(r.Context(), 1, metric.WithAttributes(attribute.String("method", in.method()), attribute.String("result", result)))
	}()
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	res, err := h.svc.SignIn(r.Context(), service.SignInInput{
		IdempotencyKey: in.IdempotencyKey, Proof: in.proof(), DeviceName: in.DeviceName,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	result = "success"
	httpx.Respond(w, http.StatusOK, res)
}
