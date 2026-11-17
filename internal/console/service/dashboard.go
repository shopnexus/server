package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/console/repository"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/token"
)

// CreateDashboardTicket gives the caller's session a ticket that opens app
// once within domain.DashboardTicketTTL. The member's role must reach the
// app's floor, and DASHBOARD_HOSTS must give the app a host.
func (s *Console) CreateDashboardTicket(ctx context.Context, caller authn.ConsoleMember, app domain.App) (DashboardTicket, error) {
	floor, known := app.Floor()
	host, configured := s.dashboardHosts[string(app)]
	if !known || !configured {
		return DashboardTicket{}, domain.ErrDashboardNotConfigured
	}
	if !domain.Role(caller.Role).AtLeast(floor) {
		return DashboardTicket{}, domain.ErrConsoleForbidden
	}
	now := s.now()
	tok, hash := token.NewOpaque("dt_")
	err := s.dashboards.CreateTicket(ctx, domain.DashboardTicket{
		TokenHash: hash, SessionID: caller.SessionID, App: app,
		CreatedAt: now, ExpiresAt: now.Add(domain.DashboardTicketTTL),
	})
	if err != nil {
		return DashboardTicket{}, fmt.Errorf("create dashboard ticket: %w", err)
	}
	callback := url.URL{Scheme: "https", Host: host, Path: "/_dashboard/callback", RawQuery: url.Values{"ticket": {tok}}.Encode()}
	return DashboardTicket{Ticket: tok, CallbackURL: callback.String()}, nil
}

// OpenDashboard spends ticket at host, the callback request's Host, and
// starts a dashboard session that ends with the ticket's console session. A
// ticket that is unknown, used, expired, for another dashboard or of a
// console session that has ended is domain.ErrInvalidDashboardTicket.
func (s *Console) OpenDashboard(ctx context.Context, host, ticket string) (DashboardOpened, error) {
	app, ok := s.dashboardAt(host)
	if !ok {
		return DashboardOpened{}, domain.ErrDashboardHostUnknown
	}
	now := s.now()
	tok, hash := token.NewOpaque("ds_")
	expiresAt, err := s.dashboards.Redeem(ctx, token.Hash(ticket), hash, app, now)
	if errors.Is(err, repository.ErrNotFound) {
		return DashboardOpened{}, domain.ErrInvalidDashboardTicket
	}
	if err != nil {
		return DashboardOpened{}, fmt.Errorf("redeem dashboard ticket: %w", err)
	}
	return DashboardOpened{Token: tok, TTL: expiresAt.Sub(now)}, nil
}

// AuthorizeDashboard checks a dashboard request on every call: the cookie's
// session must be for the dashboard at in.Host, its console session must live
// as Authenticate requires, and the member's role must reach the
// dashboard's floor. A request without such a session is not granted and
// gets the console's URL that opens the dashboard at in.URI.
func (s *Console) AuthorizeDashboard(ctx context.Context, in DashboardCheck) (DashboardAccess, error) {
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
		return DashboardAccess{}, domain.ErrConsoleForbidden
	}
	return DashboardAccess{Granted: true, Email: g.Email, Role: app.RoleFor(g.Role)}, nil
}

// DashboardLanding is where a dashboard request for in.URI goes when the
// dashboard opens: the console's OpenURL when AuthorizeDashboard does not
// grant it, and in.URI when it does. The S3 console signs its calls in the
// browser, so with StoreLogin set it lands on the console's SSO callback
// with fresh keys instead.
func (s *Console) DashboardLanding(ctx context.Context, in DashboardCheck) (string, error) {
	access, err := s.AuthorizeDashboard(ctx, in)
	if err != nil {
		return "", err
	}
	if !access.Granted {
		return access.OpenURL, nil
	}
	rd := in.URI
	if rd == "" {
		rd = "/"
	}
	if app, _ := s.dashboardAt(in.Host); app != domain.AppS3 || s.storeLogin == nil {
		return rd, nil
	}
	c, err := s.storeLogin(ctx)
	if err != nil {
		return "", fmt.Errorf("mint the S3 console's keys: %w", err)
	}
	return domain.StoreConsoleCallback(c, rd), nil
}

// dashboardAt is the dashboard DASHBOARD_HOSTS serves at host, a Host or
// X-Forwarded-Host value, whose port and case it ignores.
func (s *Console) dashboardAt(host string) (domain.App, bool) {
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
