package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type addIdentityRequest struct {
	proofBody

	Key uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// AddIdentity serves POST /v1/accounts/me/identities.
func (h *Handler) AddIdentity(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in addIdentityRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.AddIdentity(r.Context(), service.AddIdentityInput{
		Key: in.Key, Caller: p, Proof: in.proof(),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}
