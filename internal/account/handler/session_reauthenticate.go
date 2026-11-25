package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type reauthenticateRequest struct {
	proofBody

	Key uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// Reauthenticate serves POST /v1/auth/sessions/current/reauthenticate.
func (h *Handler) Reauthenticate(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in reauthenticateRequest) (domain.AccessToken, error) {
		return h.svc.Reauthenticate(r.Context(), service.ReauthenticateInput{Key: in.Key, Caller: p, Proof: in.Proof})
	})
}
