package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type enrollmentRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// EnrollTOTP serves POST /v1/staff/login/totp-enrollment.
func (h *Handler) EnrollTOTP(w http.ResponseWriter, r *http.Request) {
	var in enrollmentRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	enr, err := h.svc.EnrollTOTP(r.Context(), in.Email, in.Password)
	if err != nil {
		h.logRefusal(r, in.Email, err)
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, enr)
}
