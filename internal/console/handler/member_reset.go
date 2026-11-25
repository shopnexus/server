package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// temporaryPassword is the TemporaryPassword schema.
type temporaryPassword struct {
	TemporaryPassword string `json:"temporary_password"`
}

// ResetMemberPassword serves POST /v1/console/members/{id}/reset-password.
func (h *Handler) ResetMemberPassword(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in memberRequest) (temporaryPassword, error) {
		temp, err := h.svc.ResetMemberPassword(r.Context(), in.ID, audit.ByMember(caller))
		return temporaryPassword{TemporaryPassword: temp}, err
	})
}

// ResetMemberTOTP serves POST /v1/console/members/{id}/reset-totp.
func (h *Handler) ResetMemberTOTP(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in memberRequest) (service.MemberView, error) {
		return h.svc.ResetMemberTOTP(r.Context(), in.ID, audit.ByMember(caller))
	})
}
