// Package handler serves module console's HTTP API under /v1/console and the
// ops dashboards' callback at /_dashboard/callback.
package handler

import (
	"log/slog"
	"net/http"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

type Handler struct {
	svc service.Service
	// support works the tickets module support keeps, trust the report
	// cases, and catalog the listings members hide.
	support supportsvc.Service
	trust   trustsvc.Service
	catalog catalogsvc.Service
	log     *slog.Logger
}

func New(svc service.Service, support supportsvc.Service, trust trustsvc.Service, catalog catalogsvc.Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, support: support, trust: trust, catalog: catalog, log: log}
}

// Register adds the console endpoints. Every one under /v1/console but
// dashboard-auth needs the console's header, and all but sign-in need a
// moderator's or an admin's session. The ops dashboards' forwardAuth check
// and callback come from Traefik and from the dashboard's host, not from
// the console.
func (h *Handler) Register(r *httpx.Router) {
	console := FromConsole(h.log)
	handle := func(pattern string, hh http.Handler) { r.Handle(pattern, console(hh)) }
	mod := Require(h.svc, h.log, domain.RoleModerator)
	admin := Require(h.svc, h.log, domain.RoleAdmin)

	handle("POST /v1/console/login", http.HandlerFunc(h.Login))
	handle("POST /v1/console/login/totp-enrollment", http.HandlerFunc(h.EnrollTOTP))
	handle("POST /v1/console/logout", mod(http.HandlerFunc(h.Logout)))
	handle("GET /v1/console/me", mod(http.HandlerFunc(h.GetMe)))
	handle("PUT /v1/console/me/password", mod(http.HandlerFunc(h.ChangePassword)))

	handle("GET /v1/console/members", admin(http.HandlerFunc(h.ListMembers)))
	handle("POST /v1/console/members", admin(http.HandlerFunc(h.CreateMember)))
	handle("POST /v1/console/members/{id}/disable", admin(http.HandlerFunc(h.DisableMember)))
	handle("POST /v1/console/members/{id}/enable", admin(http.HandlerFunc(h.EnableMember)))
	handle("POST /v1/console/members/{id}/unlock", admin(http.HandlerFunc(h.UnlockMember)))
	handle("POST /v1/console/members/{id}/reset-password", admin(http.HandlerFunc(h.ResetMemberPassword)))
	handle("POST /v1/console/members/{id}/reset-totp", admin(http.HandlerFunc(h.ResetMemberTOTP)))

	handle("GET /v1/console/accounts", mod(http.HandlerFunc(h.ListAccounts)))
	handle("GET /v1/console/accounts/{id}", mod(http.HandlerFunc(h.GetAccount)))
	handle("POST /v1/console/accounts/{id}/suspend", mod(http.HandlerFunc(h.SuspendAccount)))
	handle("POST /v1/console/accounts/{id}/unsuspend", mod(http.HandlerFunc(h.UnsuspendAccount)))
	handle("GET /v1/console/actions", admin(http.HandlerFunc(h.ListActions)))

	handle("GET /v1/console/support/tickets", mod(http.HandlerFunc(h.ListTickets)))
	handle("GET /v1/console/support/tickets/{id}", mod(http.HandlerFunc(h.GetTicket)))
	handle("GET /v1/console/support/tickets/{id}/messages", mod(http.HandlerFunc(h.ListTicketMessages)))
	handle("POST /v1/console/support/tickets/{id}/replies", mod(http.HandlerFunc(h.ReplyTicket)))
	handle("POST /v1/console/support/tickets/{id}/resolve", mod(http.HandlerFunc(h.ResolveTicket)))
	handle("POST /v1/console/support/tickets/{id}/reopen", mod(http.HandlerFunc(h.ReopenTicket)))
	handle("GET /v1/console/support/summary", mod(http.HandlerFunc(h.SupportSummary)))

	handle("GET /v1/console/reports", mod(http.HandlerFunc(h.ListReportCases)))
	handle("GET /v1/console/reports/{account_id}", mod(http.HandlerFunc(h.GetReportCase)))
	handle("POST /v1/console/reports/{account_id}/dismiss", mod(http.HandlerFunc(h.DismissReportCase)))
	handle("POST /v1/console/reports/{account_id}/resolve", mod(http.HandlerFunc(h.ResolveReportCase)))
	handle("POST /v1/console/reports/{account_id}/reopen", mod(http.HandlerFunc(h.ReopenReportCase)))
	handle("POST /v1/console/listings/{id}/hide", mod(http.HandlerFunc(h.HideListing)))
	handle("POST /v1/console/listings/{id}/unhide", mod(http.HandlerFunc(h.UnhideListing)))

	handle("POST /v1/console/dashboard-tickets", mod(http.HandlerFunc(h.CreateDashboardTicket)))
	handle("GET /v1/console/settings/dashboards", admin(http.HandlerFunc(h.ListDashboardFloors)))
	handle("PATCH /v1/console/settings/dashboards", admin(http.HandlerFunc(h.SetDashboardFloors)))
	r.Handle("GET /v1/console/dashboard-auth", http.HandlerFunc(h.AuthorizeDashboard))
	r.Handle("GET /_dashboard/callback", http.HandlerFunc(h.OpenDashboard))
}
