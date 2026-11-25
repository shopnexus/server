package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// GetMe serves GET /v1/console/me.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, _ httpx.None) (service.Me, error) {
		return h.svc.GetMe(r.Context(), caller)
	})
}

// ChangePassword serves PUT /v1/console/me/password and clears the cookie of
// the session the change ended.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in changePasswordRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.ChangePassword(r.Context(), caller, in.CurrentPassword, in.NewPassword); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Logout serves POST /v1/console/logout and clears the cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	if err := h.svc.Logout(r.Context(), caller); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
