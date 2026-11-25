package handler

import (
	"math"
	"net/http"
	"strings"
	"unicode"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

const dashboardCookie = "console_dashboard"

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

// CreateDashboardTicket serves POST /v1/console/dashboard-tickets.
func (h *Handler) CreateDashboardTicket(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in dashboardTicketRequest) (service.DashboardTicket, error) {
		return h.svc.CreateDashboardTicket(r.Context(), caller, in.App)
	})
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

// AuthorizeDashboard serves GET /v1/console/dashboard-auth, Traefik's
// forwardAuth check. Traefik passes a 2xx on with the X-Console-* headers and
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
		w.Header().Set("X-Console-Email", out.Email)
		w.Header().Set("X-Console-Role", out.Role)
		w.WriteHeader(http.StatusOK)
	case in.Method == "" || in.Method == http.MethodGet:
		w.Header().Set("Location", out.OpenURL)
		w.WriteHeader(http.StatusFound)
	default:
		httpx.WriteError(w, r, h.log, authn.ErrUnauthenticated)
	}
}

type dashboardFloors struct {
	Items []domain.DashboardFloor `json:"items"`
}

// ListDashboardFloors serves GET /v1/console/settings/dashboards.
func (h *Handler) ListDashboardFloors(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, _ httpx.None) (dashboardFloors, error) {
		out, err := h.svc.DashboardFloors(r.Context())
		return dashboardFloors{out}, err
	})
}

type setFloorsRequest struct {
	Items []struct {
		App   domain.App  `json:"app"`
		Floor domain.Role `json:"floor"`
	} `json:"items"`
}

// SetDashboardFloors serves PATCH /v1/console/settings/dashboards.
func (h *Handler) SetDashboardFloors(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in setFloorsRequest) (dashboardFloors, error) {
		floors := make(map[domain.App]domain.Role, len(in.Items))
		for _, it := range in.Items {
			floors[it.App] = it.Floor
		}
		out, err := h.svc.SetDashboardFloors(r.Context(), caller, floors)
		return dashboardFloors{out}, err
	})
}
