package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type addIdentityRequest struct {
	proofBody

	Key uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// AddIdentity serves POST /v1/accounts/me/identities.
func (h *Handler) AddIdentity(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in addIdentityRequest) (service.IdentitiesView, error) {
		return h.svc.AddIdentity(r.Context(), service.AddIdentityInput{Key: in.Key, Caller: p, Proof: in.Proof})
	})
}
