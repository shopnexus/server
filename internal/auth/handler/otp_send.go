package handler

import (
	"errors"
	"net/http"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type otpSendRequest struct {
	IdempotencyKey string         `header:"Idempotency-Key" json:"-"`
	Kind           contact.Kind   `json:"kind"`
	Destination    string         `json:"destination"`
	Purpose        domain.Purpose `json:"purpose"`
}

// SendOTP serves POST /v1/auth/otp.
func (h *Handler) SendOTP(w http.ResponseWriter, r *http.Request) {
	var in otpSendRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	dest, err := contact.Parse(in.Kind, in.Destination)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	ip := httpx.ClientIP(r)
	if !ip.IsValid() {
		httpx.WriteError(w, r, h.log, errors.New("otp send: no client address"))
		return
	}
	res, err := h.svc.SendOTP(r.Context(), service.SendOTPInput{
		IdempotencyKey: in.IdempotencyKey, Destination: dest, Purpose: in.Purpose, IP: ip,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, res)
}
