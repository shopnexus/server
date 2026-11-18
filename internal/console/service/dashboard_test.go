package service_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/repository"
	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/token"
)

// ticket asks for a ticket for app with the session of in.
func (e env) ticket(t *testing.T, in service.SignedIn, app domain.App) string {
	t.Helper()
	out, err := e.at(now).CreateDashboardTicket(context.Background(), e.callerOf(t, in), app)
	if err != nil {
		t.Fatal(err)
	}
	return out.Ticket
}

// open spends a ticket at host and returns the dashboard cookie's value.
func (e env) open(t *testing.T, host, ticket string) string {
	t.Helper()
	out, err := e.at(now).OpenDashboard(context.Background(), host, ticket)
	if err != nil {
		t.Fatal(err)
	}
	return out.Token
}

func TestDashboardTicketFloors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, mod := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	_, admin := e.enrolled(t, "admin@example.com", domain.RoleAdmin)

	out, err := e.at(now).CreateDashboardTicket(ctx, e.callerOf(t, mod), domain.AppGrafana)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(out.CallbackURL)
	if err != nil || u.Scheme != "https" || u.Host != "grafana.test" || u.Path != "/_dashboard/callback" ||
		u.Query().Get("ticket") != out.Ticket || out.Ticket == "" {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = e.at(now).CreateDashboardTicket(ctx, e.callerOf(t, mod), domain.AppRestate); !errors.Is(err, domain.ErrConsoleForbidden) {
		t.Fatalf("a moderator for restate: %v", err)
	}
	if _, err = e.at(now).CreateDashboardTicket(ctx, e.callerOf(t, admin), domain.AppRestate); err != nil {
		t.Fatalf("an admin for restate: %v", err)
	}
	for _, app := range []domain.App{domain.AppLiteLLM, "kibana"} {
		if _, err = e.at(now).CreateDashboardTicket(ctx, e.callerOf(t, admin), app); !errors.Is(err, domain.ErrDashboardNotConfigured) {
			t.Fatalf("%s: %v", app, err)
		}
	}
}

func TestOpenDashboardTakesTheTicketOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, mod := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	ticket := e.ticket(t, mod, domain.AppGrafana)

	if _, err := e.at(now).OpenDashboard(ctx, "elsewhere.test", ticket); !errors.Is(err, domain.ErrDashboardHostUnknown) {
		t.Fatalf("an unknown host: %v", err)
	}
	if _, err := e.at(now).OpenDashboard(ctx, "restate.test", ticket); !errors.Is(err, domain.ErrInvalidDashboardTicket) {
		t.Fatalf("another dashboard's host: %v", err)
	}
	later := now.Add(10 * time.Second)
	out, err := e.at(later).OpenDashboard(ctx, "Grafana.test:443", ticket)
	if err != nil || out.Token == "" || out.TTL != domain.SessionTTL-10*time.Second {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = e.at(later).OpenDashboard(ctx, "grafana.test", ticket); !errors.Is(err, domain.ErrInvalidDashboardTicket) {
		t.Fatalf("a second time: %v", err)
	}
	if _, err = e.at(now).OpenDashboard(ctx, "grafana.test", "dt_unknown"); !errors.Is(err, domain.ErrInvalidDashboardTicket) {
		t.Fatalf("an unknown ticket: %v", err)
	}
}

func TestDashboardTicketExpires(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, mod := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	ticket := e.ticket(t, mod, domain.AppGrafana)
	if _, err := e.at(now.Add(domain.DashboardTicketTTL)).OpenDashboard(ctx, "grafana.test", ticket); !errors.Is(err, domain.ErrInvalidDashboardTicket) {
		t.Fatalf("after 60 seconds: %v", err)
	}
	if _, err := e.at(now.Add(domain.DashboardTicketTTL-time.Second)).OpenDashboard(ctx, "grafana.test", ticket); err != nil {
		t.Fatalf("within 60 seconds: %v", err)
	}
}

func TestAuthorizeDashboard(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, mod := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	_, admin := e.enrolled(t, "admin@example.com", domain.RoleAdmin)
	modGrafana := e.open(t, "grafana.test", e.ticket(t, mod, domain.AppGrafana))
	adminGrafana := e.open(t, "grafana.test", e.ticket(t, admin, domain.AppGrafana))
	adminRestate := e.open(t, "restate.test", e.ticket(t, admin, domain.AppRestate))

	cases := []struct {
		host, token, email, role string
	}{
		{"grafana.test", modGrafana, "mod@example.com", "Viewer"},
		{"grafana.test:443", adminGrafana, "admin@example.com", "Admin"},
		{"restate.test", adminRestate, "admin@example.com", "admin"},
	}
	for _, c := range cases {
		got, err := e.at(now).AuthorizeDashboard(ctx, service.DashboardCheck{Host: c.host, URI: "/", Token: c.token})
		if err != nil || !got.Granted || got.Email != c.email || got.Role != c.role {
			t.Errorf("%s as %s: %+v %v", c.host, c.email, got, err)
		}
	}

	for name, token := range map[string]string{"no cookie": "", "an unknown cookie": "ds_unknown", "grafana's cookie": adminGrafana} {
		got, err := e.at(now).AuthorizeDashboard(ctx, service.DashboardCheck{Host: "restate.test", URI: "/ui/invocations?x=1&y=2", Token: token})
		want := "https://console.test/dashboards/open?app=restate&rd=" + url.QueryEscape("/ui/invocations?x=1&y=2")
		if err != nil || got.Granted || got.OpenURL != want {
			t.Errorf("%s: %+v %v", name, got, err)
		}
	}
	if got, _ := e.at(now).AuthorizeDashboard(ctx, service.DashboardCheck{Host: "grafana.test", Token: ""}); !strings.HasSuffix(got.OpenURL, "rd=%2F") {
		t.Errorf("no uri: %q", got.OpenURL)
	}
	if _, err := e.at(now).AuthorizeDashboard(ctx, service.DashboardCheck{Host: "elsewhere.test", Token: modGrafana}); !errors.Is(err, domain.ErrDashboardHostUnknown) {
		t.Fatalf("an unknown host: %v", err)
	}
}

// The ticket endpoint never gives a moderator restate, but a role below the
// floor is refused at every request too.
func TestAuthorizeDashboardHoldsTheFloor(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, mod := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	tok, hash := token.NewOpaque("dt_")
	err := repository.NewDashboards(e.db).CreateTicket(ctx, domain.DashboardTicket{
		TokenHash: hash, SessionID: e.callerOf(t, mod).SessionID, App: domain.AppRestate,
		CreatedAt: now, ExpiresAt: now.Add(domain.DashboardTicketTTL),
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie := e.open(t, "restate.test", tok)
	if _, err = e.at(now).AuthorizeDashboard(ctx, service.DashboardCheck{Host: "restate.test", Token: cookie}); !errors.Is(err, domain.ErrConsoleForbidden) {
		t.Fatalf("a moderator on restate: %v", err)
	}
}

// Signing out, a disable and the console session's end close every dashboard
// opened from the session.
func TestDashboardSessionsEndWithTheConsoleSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	granted := func(at time.Time, cookie string) bool {
		t.Helper()
		got, err := e.at(at).AuthorizeDashboard(ctx, service.DashboardCheck{Host: "grafana.test", Token: cookie})
		if err != nil {
			t.Fatal(err)
		}
		return got.Granted
	}

	_, out := e.enrolled(t, "out@example.com", domain.RoleModerator)
	cookie := e.open(t, "grafana.test", e.ticket(t, out, domain.AppGrafana))
	pending := e.ticket(t, out, domain.AppGrafana)
	if !granted(now, cookie) || granted(now.Add(domain.SessionTTL), cookie) {
		t.Fatal("the dashboard does not follow the console session's end")
	}
	if err := e.at(now).Logout(ctx, e.callerOf(t, out)); err != nil {
		t.Fatal(err)
	}
	if granted(now, cookie) {
		t.Fatal("granted after a sign-out")
	}
	if _, err := e.at(now).OpenDashboard(ctx, "grafana.test", pending); !errors.Is(err, domain.ErrInvalidDashboardTicket) {
		t.Fatalf("a ticket after a sign-out: %v", err)
	}

	_, off := e.enrolled(t, "off@example.com", domain.RoleModerator)
	cookie = e.open(t, "grafana.test", e.ticket(t, off, domain.AppGrafana))
	if _, err := e.at(now).DisableMember(ctx, off.Me.ID, audit.ByCLI("test")); err != nil {
		t.Fatal(err)
	}
	if granted(now, cookie) {
		t.Fatal("granted after a disable")
	}
}

// The floors are console_dashboards': a moderator is refused restate while
// its floor is admin, both for a ticket and on the dashboard session the
// moderator already holds, and let in once an admin lowers it; raising it
// again refuses the same session on its next request. Each change is a
// console action.
func TestDashboardFloorsAreTheTables(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	_, mod := e.enrolled(t, "mod@example.com", domain.RoleModerator)
	_, admin := e.enrolled(t, "admin@example.com", domain.RoleAdmin)
	s, modCaller, adminCaller := e.at(now), e.callerOf(t, mod), e.callerOf(t, admin)

	if _, err := s.CreateDashboardTicket(ctx, modCaller, domain.AppRestate); !errors.Is(err, domain.ErrConsoleForbidden) {
		t.Fatalf("a moderator for restate at admin: %v", err)
	}
	floors, err := s.SetDashboardFloors(ctx, adminCaller, map[domain.App]domain.Role{domain.AppRestate: domain.RoleModerator})
	if err != nil || len(floors) != 6 || floors[1].App != domain.AppRestate || floors[1].Floor != domain.RoleModerator ||
		floors[1].Name != "Restate" {
		t.Fatalf("floors %+v %v", floors, err)
	}
	cookie := e.open(t, "restate.test", e.ticket(t, mod, domain.AppRestate))
	if got, authErr := s.AuthorizeDashboard(ctx, service.DashboardCheck{Host: "restate.test", Token: cookie}); authErr != nil || !got.Granted {
		t.Fatalf("a moderator on restate at moderator: %+v %v", got, authErr)
	}
	if _, err = s.SetDashboardFloors(ctx, adminCaller, map[domain.App]domain.Role{domain.AppRestate: domain.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthorizeDashboard(ctx, service.DashboardCheck{Host: "restate.test", Token: cookie}); !errors.Is(err, domain.ErrConsoleForbidden) {
		t.Fatalf("the moderator's session after the floor went up: %v", err)
	}
	for floors, want := range map[domain.App]domain.Role{"kibana": domain.RoleAdmin, domain.AppGrafana: "owner"} {
		if _, err = s.SetDashboardFloors(ctx, adminCaller, map[domain.App]domain.Role{floors: want}); err == nil {
			t.Errorf("%s at %s passed", floors, want)
		}
	}
	var n int
	if err = e.db.QueryRow(ctx, `SELECT count(*) FROM console_actions WHERE action_kind = 'dashboard.floor'`).Scan(&n); err != nil || n != 2 {
		t.Errorf("%d dashboard.floor actions, %v", n, err)
	}
}
