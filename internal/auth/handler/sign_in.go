package handler

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type signInRequest struct {
	IdempotencyKey string `header:"Idempotency-Key" json:"-"`
	PhoneToken     string `json:"phone_token"`
	DeviceName     string `json:"device_name"`
}

// SignIn serves POST /v1/auth/sign-in. Every request counts in login_total,
// a malformed one as a failure.
func (h *Handler) SignIn(w http.ResponseWriter, r *http.Request) {
	result := "failure"
	defer func() {
		logins.Add(r.Context(), 1, metric.WithAttributes(attribute.String("method", "phone"), attribute.String("result", result)))
	}()
	var in signInRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	res, err := h.svc.SignIn(r.Context(), service.SignInInput{
		IdempotencyKey: in.IdempotencyKey, PhoneToken: in.PhoneToken, DeviceName: in.DeviceName,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	result = "success"
	httpx.Respond(w, http.StatusOK, res)
}
