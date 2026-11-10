package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

// DeleteCurrentSession serves DELETE /v1/auth/sessions/current.
func (h *Handler) DeleteCurrentSession(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	if err := h.svc.DeleteCurrentSession(r.Context(), p); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteAllSessions serves DELETE /v1/auth/sessions.
func (h *Handler) DeleteAllSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	if err := h.svc.DeleteAllSessions(r.Context(), p); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deleteSessionRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// DeleteSession serves DELETE /v1/auth/sessions/{id}.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in deleteSessionRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.DeleteSession(r.Context(), p, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
