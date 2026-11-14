package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/platform/httpx"
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

// settingsRequest is the whole NotificationSettings document.
type settingsRequest struct {
	domain.Settings
}

// PutSettings serves PUT /v1/me/notification-settings.
func (h *Handler) PutSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in settingsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.PutSettings(r.Context(), p.AccountID, in.Settings)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}
