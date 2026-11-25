package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type emailCodeRequest struct {
	Key   uuid.UUID `header:"Idempotency-Key" json:"-"`
	Email string    `json:"email"`
}

// SendEmailCode serves POST /v1/auth/email-codes.
func (h *Handler) SendEmailCode(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in emailCodeRequest) (service.EmailCodeView, error) {
		return h.svc.SendEmailCode(r.Context(), service.SendEmailCodeInput{
			Key: in.Key, Email: in.Email, IP: httpx.ClientIP(r),
			Locale: locale.FromRequest(r),
		})
	})
}

// SendMyEmailCode serves POST /v1/accounts/me/email-codes.
func (h *Handler) SendMyEmailCode(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in emailCodeRequest) (service.EmailCodeView, error) {
		return h.svc.SendEmailCode(r.Context(), service.SendEmailCodeInput{
			Key: in.Key, Email: in.Email, Caller: &p, IP: httpx.ClientIP(r),
			Locale: locale.FromRequest(r),
		})
	})
}
