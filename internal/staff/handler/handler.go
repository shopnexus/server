// Package handler serves module staff's HTTP API under /v1/staff and the
// ops dashboards' callback at /_dashboard/callback.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/service"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the staff endpoints. Every one under /v1/staff but
// dashboard-auth needs the console's header, and all but sign-in need a
// moderator's or an admin's session. The ops dashboards' forwardAuth check
// and callback come from Traefik and from the dashboard's host, not from
// the console.
func (h *Handler) Register(r *httpx.Router) {
	console := FromConsole(h.log)
	handle := func(pattern string, hh http.Handler) { r.Handle(pattern, console(hh)) }
	mod := Require(h.svc, h.log, domain.RoleModerator)
	admin := Require(h.svc, h.log, domain.RoleAdmin)

	handle("POST /v1/staff/login", http.HandlerFunc(h.Login))
	handle("POST /v1/staff/login/totp-enrollment", http.HandlerFunc(h.EnrollTOTP))
	handle("POST /v1/staff/logout", mod(http.HandlerFunc(h.Logout)))
	handle("GET /v1/staff/me", mod(http.HandlerFunc(h.GetMe)))
	handle("PUT /v1/staff/me/password", mod(http.HandlerFunc(h.ChangePassword)))

	handle("GET /v1/staff/members", admin(http.HandlerFunc(h.ListMembers)))
	handle("POST /v1/staff/members", admin(http.HandlerFunc(h.CreateMember)))
	handle("POST /v1/staff/members/{id}/disable", admin(http.HandlerFunc(h.DisableMember)))
	handle("POST /v1/staff/members/{id}/enable", admin(http.HandlerFunc(h.EnableMember)))
	handle("POST /v1/staff/members/{id}/unlock", admin(http.HandlerFunc(h.UnlockMember)))
	handle("POST /v1/staff/members/{id}/reset-password", admin(http.HandlerFunc(h.ResetMemberPassword)))
	handle("POST /v1/staff/members/{id}/reset-totp", admin(http.HandlerFunc(h.ResetMemberTOTP)))

	handle("GET /v1/staff/accounts", mod(http.HandlerFunc(h.ListAccounts)))
	handle("GET /v1/staff/accounts/{id}", mod(http.HandlerFunc(h.GetAccount)))
	handle("POST /v1/staff/accounts/{id}/suspend", mod(http.HandlerFunc(h.SuspendAccount)))
	handle("POST /v1/staff/accounts/{id}/unsuspend", mod(http.HandlerFunc(h.UnsuspendAccount)))
	handle("GET /v1/staff/actions", admin(http.HandlerFunc(h.ListActions)))

	handle("POST /v1/staff/dashboard-tickets", mod(http.HandlerFunc(h.CreateDashboardTicket)))
	r.Handle("GET /v1/staff/dashboard-auth", http.HandlerFunc(h.AuthorizeDashboard))
	r.Handle("GET /_dashboard/callback", http.HandlerFunc(h.OpenDashboard))
}
