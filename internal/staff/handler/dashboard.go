package handler

import (
	"math"
	"net/http"
	"strings"
	"unicode"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/service"
)

const dashboardCookie = "staff_dashboard"

type dashboardTicketRequest struct {
	App domain.App `json:"app"`
}

type openDashboardRequest struct {
	Ticket string `json:"-" query:"ticket"`
	RD     string `json:"-" query:"rd"`
}

type authorizeDashboardRequest struct {
	Host   string `header:"X-Forwarded-Host"   json:"-"`
	URI    string `header:"X-Forwarded-Uri"    json:"-"`
	Method string `header:"X-Forwarded-Method" json:"-"`
}

// CreateDashboardTicket serves POST /v1/staff/dashboard-tickets.
func (h *Handler) CreateDashboardTicket(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.StaffCaller(w, r, h.log)
	if !ok {
		return
	}
	var in dashboardTicketRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.CreateDashboardTicket(r.Context(), caller, in.App)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// OpenDashboard serves GET /_dashboard/callback on a dashboard's host. The
// cookie has no Domain, so only this host gets it, and SameSite=Lax, so the
// redirect from the console carries it on the first page. rd is followed
// only as a path on this host: a leading "//" or "/\", or a tab or newline
// a browser strips, would make it another host.
func (h *Handler) OpenDashboard(w http.ResponseWriter, r *http.Request) {
	var in openDashboardRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.OpenDashboard(r.Context(), r.Host, in.Ticket)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: dashboardCookie, Value: out.Token, Path: "/", MaxAge: int(math.Ceil(out.TTL.Seconds())),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	rd := in.RD
	if !strings.HasPrefix(rd, "/") || strings.HasPrefix(rd, "//") ||
		strings.ContainsFunc(rd, func(c rune) bool { return c == '\\' || unicode.IsControl(c) }) {
		rd = "/"
	}
	w.Header().Set("Location", rd)
	w.WriteHeader(http.StatusFound)
}

// AuthorizeDashboard serves GET /v1/staff/dashboard-auth, Traefik's
// forwardAuth check. Traefik passes a 2xx on with the X-Staff-* headers and
// returns any other answer to the browser: a GET without a session goes to
// the console for a ticket, and another method, which a redirect would turn
// into a GET, gets 401.
func (h *Handler) AuthorizeDashboard(w http.ResponseWriter, r *http.Request) {
	var in authorizeDashboardRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	var tok string
	if c, err := r.Cookie(dashboardCookie); err == nil {
		tok = c.Value
	}
	out, err := h.svc.AuthorizeDashboard(r.Context(), service.DashboardCheck{Host: in.Host, URI: in.URI, Token: tok})
	switch {
	case err != nil:
		httpx.WriteError(w, r, h.log, err)
	case out.Granted:
		w.Header().Set("X-Staff-Email", out.Email)
		w.Header().Set("X-Staff-Role", out.Role)
		w.WriteHeader(http.StatusOK)
	case in.Method == "" || in.Method == http.MethodGet:
		w.Header().Set("Location", out.OpenURL)
		w.WriteHeader(http.StatusFound)
	default:
		httpx.WriteError(w, r, h.log, authn.ErrUnauthenticated)
	}
}
