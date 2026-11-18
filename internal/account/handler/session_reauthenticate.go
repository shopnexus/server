package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type reauthenticateRequest struct {
	proofBody

	IdempotencyKey string `header:"Idempotency-Key" json:"-"`
}

// Reauthenticate serves POST /v1/auth/sessions/current/reauthenticate.
func (h *Handler) Reauthenticate(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in reauthenticateRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	res, err := h.svc.Reauthenticate(r.Context(), service.ReauthenticateInput{
		IdempotencyKey: in.IdempotencyKey, Caller: p, Proof: in.proof(),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, res)
}
