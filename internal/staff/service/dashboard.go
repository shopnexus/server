package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/token"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/repository"
)

// CreateDashboardTicket gives the caller's session a ticket that opens app
// once within domain.DashboardTicketTTL. The member's role must reach the
// app's floor, and DASHBOARD_HOSTS must give the app a host.
func (s *Staff) CreateDashboardTicket(ctx context.Context, caller authn.Staff, app domain.App) (DashboardTicket, error) {
	floor, known := app.Floor()
	host, configured := s.dashboardHosts[string(app)]
	if !known || !configured {
		return DashboardTicket{}, domain.ErrDashboardNotConfigured
	}
	m, err := s.members.Get(ctx, caller.StaffID)
	if err != nil {
		return DashboardTicket{}, fmt.Errorf("get staff member: %w", err)
	}
	if !m.Role().AtLeast(floor) {
		return DashboardTicket{}, domain.ErrStaffForbidden
	}
	now := s.now()
	tok, hash := token.NewOpaque("dt_")
	err = s.dashboards.CreateTicket(ctx, domain.DashboardTicket{
		TokenHash: hash, StaffSessionID: caller.SessionID, App: app,
		CreatedAt: now, ExpiresAt: now.Add(domain.DashboardTicketTTL),
	})
	if err != nil {
		return DashboardTicket{}, fmt.Errorf("create dashboard ticket: %w", err)
	}
	callback := url.URL{Scheme: "https", Host: host, Path: "/_dashboard/callback", RawQuery: url.Values{"ticket": {tok}}.Encode()}
	return DashboardTicket{Ticket: tok, CallbackURL: callback.String()}, nil
}

// OpenDashboard spends ticket at host, the callback request's Host, and
// starts a dashboard session that ends with the ticket's staff session. A
// ticket that is unknown, used, expired, for another dashboard or of a
// staff session that has ended is domain.ErrInvalidDashboardTicket.
func (s *Staff) OpenDashboard(ctx context.Context, host, ticket string) (DashboardOpened, error) {
	app, ok := s.dashboardAt(host)
	if !ok {
		return DashboardOpened{}, domain.ErrDashboardHostUnknown
	}
	now := s.now()
	tok, hash := token.NewOpaque("ds_")
	ds, err := s.dashboards.Redeem(ctx, token.Hash(ticket), domain.DashboardSession{TokenHash: hash, App: app, CreatedAt: now})
	if errors.Is(err, repository.ErrNotFound) {
		return DashboardOpened{}, domain.ErrInvalidDashboardTicket
	}
	if err != nil {
		return DashboardOpened{}, fmt.Errorf("redeem dashboard ticket: %w", err)
	}
	return DashboardOpened{Token: tok, TTL: ds.ExpiresAt.Sub(now)}, nil
}

// AuthorizeDashboard checks a dashboard request on every call: the cookie's
// session must be for the dashboard at in.Host, its staff session must live
// as Authenticate requires, and the member's role must reach the
// dashboard's floor. A request without such a session is not granted and
// gets the console's URL that opens the dashboard at in.URI.
func (s *Staff) AuthorizeDashboard(ctx context.Context, in DashboardCheck) (DashboardAccess, error) {
	app, ok := s.dashboardAt(in.Host)
	if !ok {
		return DashboardAccess{}, domain.ErrDashboardHostUnknown
	}
	g, err := s.dashboards.Live(ctx, token.Hash(in.Token), app, s.now())
	if errors.Is(err, repository.ErrNotFound) {
		rd := in.URI
		if rd == "" {
			rd = "/"
		}
		open := s.consoleOrigin + "/dashboards/open?" + url.Values{"app": {string(app)}, "rd": {rd}}.Encode()
		return DashboardAccess{OpenURL: open}, nil
	}
	if err != nil {
		return DashboardAccess{}, fmt.Errorf("read dashboard session: %w", err)
	}
	if floor, _ := app.Floor(); !g.Role.AtLeast(floor) {
		return DashboardAccess{}, domain.ErrStaffForbidden
	}
	return DashboardAccess{Granted: true, Email: g.Email, Role: app.RoleFor(g.Role)}, nil
}

// dashboardAt is the dashboard DASHBOARD_HOSTS serves at host, a Host or
// X-Forwarded-Host value, whose port and case it ignores.
func (s *Staff) dashboardAt(host string) (domain.App, bool) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	for app, h := range s.dashboardHosts {
		if h == host {
			return domain.App(app), true
		}
	}
	return "", false
}
