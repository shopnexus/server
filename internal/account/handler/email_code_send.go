package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type emailCodeRequest struct {
	IdempotencyKey string `header:"Idempotency-Key" json:"-"`
	Email          string `json:"email"`
}

// SendEmailCode serves POST /v1/auth/email-codes.
func (h *Handler) SendEmailCode(w http.ResponseWriter, r *http.Request) {
	var in emailCodeRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.SendEmailCode(r.Context(), service.SendEmailCodeInput{
		IdempotencyKey: in.IdempotencyKey, Email: in.Email, IP: httpx.ClientIP(r),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// SendMyEmailCode serves POST /v1/accounts/me/email-codes.
func (h *Handler) SendMyEmailCode(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in emailCodeRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.SendEmailCode(r.Context(), service.SendEmailCodeInput{
		IdempotencyKey: in.IdempotencyKey, Email: in.Email, AccountID: &p.AccountID, IP: httpx.ClientIP(r),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}
