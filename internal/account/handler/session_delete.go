package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// DeleteCurrentSession serves DELETE /v1/auth/sessions/current.
func (h *Handler) DeleteCurrentSession(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, _ httpx.None) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteCurrentSession(r.Context(), p)
	})
}

// DeleteOtherSessions serves DELETE /v1/auth/sessions/others.
func (h *Handler) DeleteOtherSessions(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, _ httpx.None) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteOtherSessions(r.Context(), p)
	})
}

type deleteSessionRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// DeleteSession serves DELETE /v1/auth/sessions/{id}.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in deleteSessionRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteSession(r.Context(), p, in.ID)
	})
}
