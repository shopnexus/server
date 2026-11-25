package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type conversationRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// DismissScamWarning serves POST /v1/conversations/{id}/scam-warning/dismiss.
func (h *Handler) DismissScamWarning(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in conversationRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.DismissScamWarning(r.Context(), p.AccountID, in.ID)
	})
}
