package handler

import (
	"errors"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/service"
)

type loginRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	NewPassword string `json:"new_password"`
	Code        string `json:"code"`
	UserAgent   string `header:"User-Agent" json:"-"`
}

// Login serves POST /v1/staff/login and sets the session cookie.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Login(r.Context(), service.LoginInput{
		Email: in.Email, Password: in.Password, NewPassword: in.NewPassword, Code: in.Code,
		UserAgent: in.UserAgent, IP: httpx.ClientIP(r),
	})
	if err != nil {
		h.logRefusal(r, in.Email, err)
		httpx.WriteError(w, r, h.log, err)
		return
	}
	setSessionCookie(w, out.Token, domain.SessionTTL)
	httpx.Respond(w, http.StatusOK, out.Me)
}

// logRefusal logs a wrong password or code and a locked email, the
// sign-in events worth reading after an attack.
func (h *Handler) logRefusal(r *http.Request, email string, err error) {
	if errors.Is(err, domain.ErrInvalidCredentials) || errors.Is(err, domain.ErrInvalidTOTPCode) || errors.Is(err, domain.ErrLoginLocked) {
		h.log.WarnContext(r.Context(), "security: staff sign-in refused",
			"email", contact.NormalizeEmail(email), "ip", httpx.ClientIP(r), "err", err)
	}
}
