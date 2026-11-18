package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// Settings serves GET /v1/me/notification-settings.
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.Settings(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// PutSettings serves PUT /v1/me/notification-settings.
func (h *Handler) PutSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in domain.Settings
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.PutSettings(r.Context(), p.AccountID, in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, in)
}
