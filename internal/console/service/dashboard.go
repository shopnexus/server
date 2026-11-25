package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/token"
)

// CreateDashboardTicket gives the caller's session a ticket that opens app
// once within domain.DashboardTicketTTL. The member's role must reach the
// app's floor, and DASHBOARD_HOSTS must give the app a host.
func (s *Console) CreateDashboardTicket(ctx context.Context, caller authn.ConsoleMember, app domain.App) (DashboardTicket, error) {
	host, configured := s.d.DashboardHosts[string(app)]
	if !configured {
		return DashboardTicket{}, domain.ErrDashboardNotConfigured
	}
	floor, err := s.floorOf(ctx, app)
	if err != nil {
		return DashboardTicket{}, err
	}
	if !domain.Role(caller.Role).AtLeast(floor) {
		return DashboardTicket{}, domain.ErrConsoleForbidden
	}
	now := s.d.Now()
	tok, hash := token.NewOpaque("dt_")
	err = s.d.Dashboards.CreateTicket(ctx, domain.DashboardTicket{
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
	now := s.d.Now()
	tok, hash := token.NewOpaque("ds_")
	expiresAt, err := s.d.Dashboards.Redeem(ctx, token.Hash(ticket), hash, app, now)
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
	g, err := s.d.Dashboards.Live(ctx, token.Hash(in.Token), app, s.d.Now())
	if errors.Is(err, repository.ErrNotFound) {
		rd := in.URI
		if rd == "" {
			rd = "/"
		}
		open := s.d.ConsoleOrigin + "/dashboards/open?" + url.Values{"app": {string(app)}, "rd": {rd}}.Encode()
		return DashboardAccess{OpenURL: open}, nil
	}
	if err != nil {
		return DashboardAccess{}, fmt.Errorf("read dashboard session: %w", err)
	}
	floor, err := s.floorOf(ctx, app)
	if err != nil {
		return DashboardAccess{}, err
	}
	if !g.Role.AtLeast(floor) {
		return DashboardAccess{}, domain.ErrConsoleForbidden
	}
	return DashboardAccess{Granted: true, Email: g.Email, Role: app.RoleFor(g.Role)}, nil
}

// dashboardAt is the dashboard DASHBOARD_HOSTS serves at host, a Host or
// X-Forwarded-Host value, whose port and case it ignores.
func (s *Console) dashboardAt(host string) (domain.App, bool) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	for app, h := range s.d.DashboardHosts {
		if h == host {
			return domain.App(app), true
		}
	}
	return "", false
}

// floorOf is app's floor in console_dashboards; an app with no row, or
// one the code does not know, is domain.ErrDashboardNotConfigured.
func (s *Console) floorOf(ctx context.Context, app domain.App) (domain.Role, error) {
	if _, known := app.Floor(); !known {
		return "", domain.ErrDashboardNotConfigured
	}
	floor, err := s.d.Dashboards.Floor(ctx, app)
	if errors.Is(err, repository.ErrNotFound) {
		return "", domain.ErrDashboardNotConfigured
	}
	if err != nil {
		return "", fmt.Errorf("read dashboard floor: %w", err)
	}
	return floor, nil
}

// DashboardFloors lists the ops dashboards and their floors.
func (s *Console) DashboardFloors(ctx context.Context) ([]domain.DashboardFloor, error) {
	out, err := s.d.Dashboards.Floors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dashboard floors: %w", err)
	}
	return out, nil
}

// SetDashboardFloors sets the floors of the dashboards in floors, each
// change a console action; the tickets and the forwardAuth check read the
// new floor at once, so a dashboard session of a member below it is
// refused on its next request.
func (s *Console) SetDashboardFloors(ctx context.Context, caller authn.ConsoleMember, floors map[domain.App]domain.Role,
) ([]domain.DashboardFloor, error) {
	now := s.d.Now()
	entries := make([]audit.Entry, 0, len(floors))
	for app, floor := range floors {
		if _, known := app.Floor(); !known {
			return nil, domain.ErrDashboardNotConfigured
		}
		if floor != domain.RoleModerator && floor != domain.RoleAdmin {
			return nil, domain.ErrDashboardFloorInvalid
		}
		entries = append(entries, audit.Entry{
			Actor: audit.ByMember(caller), Action: audit.DashboardFloor, Target: audit.Dashboard(string(app)),
			Details: map[string]any{"app": string(app), "floor": string(floor)}, At: now,
		})
	}
	err := s.d.Dashboards.SetFloors(ctx, floors, caller.MemberID, now, entries)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, domain.ErrDashboardNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("set dashboard floors: %w", err)
	}
	return s.DashboardFloors(ctx)
}
