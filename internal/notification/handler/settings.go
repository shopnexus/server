package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// Settings serves GET /v1/me/notification-settings.
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (domain.Settings, error) {
		return h.svc.Settings(r.Context(), p.AccountID)
	})
}

// PutSettings serves PUT /v1/me/notification-settings.
func (h *Handler) PutSettings(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in domain.Settings) (domain.Settings, error) {
		if err := h.svc.PutSettings(r.Context(), p.AccountID, in); err != nil {
			return domain.Settings{}, err
		}
		return in, nil
	})
}
