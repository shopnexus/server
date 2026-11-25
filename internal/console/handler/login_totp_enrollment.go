package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type enrollmentRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// EnrollTOTP serves POST /v1/console/login/totp-enrollment.
func (h *Handler) EnrollTOTP(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in enrollmentRequest) (service.Enrollment, error) {
		enr, err := h.svc.EnrollTOTP(r.Context(), in.Email, in.Password)
		if err != nil {
			h.logRefusal(r, in.Email, err)
		}
		return enr, err
	})
}
