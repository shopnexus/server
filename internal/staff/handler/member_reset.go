package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// temporaryPassword is the TemporaryPassword schema.
type temporaryPassword struct {
	TemporaryPassword string `json:"temporary_password"`
}

// ResetMemberPassword serves POST /v1/staff/members/{id}/reset-password.
func (h *Handler) ResetMemberPassword(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.StaffCaller(w, r, h.log)
	if !ok {
		return
	}
	var in memberRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	temp, err := h.svc.ResetMemberPassword(r.Context(), in.ID, audit.ByStaff(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, temporaryPassword{TemporaryPassword: temp})
}

// ResetMemberTOTP serves POST /v1/staff/members/{id}/reset-totp.
func (h *Handler) ResetMemberTOTP(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.StaffCaller(w, r, h.log)
	if !ok {
		return
	}
	var in memberRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.ResetMemberTOTP(r.Context(), in.ID, audit.ByStaff(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}
