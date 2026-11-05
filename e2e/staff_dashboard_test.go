package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	staffdomain "github.com/shopnexus/server2/internal/staff/domain"
)

type dashboardTicket struct {
	Ticket      string `json:"ticket"`
	CallbackURL string `json:"callback_url"`
}

// openDashboard takes s from the console to app's host the way a browser
// goes: a ticket, then the callback on the dashboard's host. It returns the
// staff_dashboard cookie.
func openDashboard(t *testing.T, s staffSession, app, host string) *http.Cookie {
	t.Helper()
	res := staffCall(t, s, "POST", "/v1/staff/dashboard-tickets", map[string]string{"app": app})
	if res.Status != 200 {
		t.Fatalf("ticket: %d %s", res.Status, res.Body)
	}
	tk := decode[dashboardTicket](t, res)
	if tk.CallbackURL != "https://"+host+"/_dashboard/callback?ticket="+url.QueryEscape(tk.Ticket) {
		t.Fatalf("callback_url %q", tk.CallbackURL)
	}
	res = call(t, server, "GET", "/_dashboard/callback?ticket="+url.QueryEscape(tk.Ticket)+"&rd=%2Fd%2Fabc%3Ffrom%3Dnow-1h", nil, [2]string{"Host", host})
	if res.Status != 302 || res.Header.Get("Location") != "/d/abc?from=now-1h" {
		t.Fatalf("callback: %d %v %s", res.Status, res.Header, res.Body)
	}
	for _, c := range (&http.Response{Header: res.Header}).Cookies() {
		if c.Name == "staff_dashboard" {
			return c
		}
	}
	t.Fatalf("no staff_dashboard cookie in %v", res.Header)
	return nil
}

// dashboardAuth calls the forwardAuth check as Traefik does for a request
// to host.
func dashboardAuth(t *testing.T, host, method, cookie string) Response {
	t.Helper()
	h := [][2]string{{"X-Forwarded-Host", host}, {"X-Forwarded-Uri", "/d/abc?x=1"}, {"X-Forwarded-Method", method}}
	if cookie != "" {
		h = append(h, [2]string{"Cookie", "staff_dashboard=" + cookie})
	}
	return call(t, server, "GET", "/v1/staff/dashboard-auth", nil, h...)
}

func TestStaffOpensGrafana(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, staffdomain.RoleModerator)
	c := openDashboard(t, s, "grafana", "grafana.e2e.test")
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" ||
		c.MaxAge <= 0 || c.MaxAge > int(staffdomain.SessionTTL.Seconds()) {
		t.Fatalf("cookie %+v", c)
	}

	res := dashboardAuth(t, "grafana.e2e.test:443", "POST", c.Value)
	if res.Status != 200 || res.Header.Get("X-Staff-Email") != s.Email || res.Header.Get("X-Staff-Role") != "Viewer" {
		t.Fatalf("with the cookie: %d %v", res.Status, res.Header)
	}
	res = dashboardAuth(t, "grafana.e2e.test", "GET", "")
	if res.Status != 302 || res.Header.Get("Location") != "http://console.e2e/dashboards/open?app=grafana&rd=%2Fd%2Fabc%3Fx%3D1" {
		t.Fatalf("no cookie, GET: %d %v", res.Status, res.Header)
	}
	if res = dashboardAuth(t, "grafana.e2e.test", "POST", ""); res.Status != 401 {
		t.Fatalf("no cookie, POST: %d", res.Status)
	}
	if res = dashboardAuth(t, "restate.e2e.test", "GET", c.Value); res.Status != 302 {
		t.Fatalf("grafana's cookie on restate: %d", res.Status)
	}
	if res = dashboardAuth(t, "kibana.e2e.test", "GET", c.Value); res.Status != 403 || errorOf(t, res).Code != "dashboard_host_unknown" {
		t.Fatalf("an unknown host: %d %s", res.Status, res.Body)
	}

	if res = staffCall(t, s, "POST", "/v1/staff/logout", nil); res.Status != 204 {
		t.Fatalf("logout: %d", res.Status)
	}
	if res = dashboardAuth(t, "grafana.e2e.test", "GET", c.Value); res.Status != 302 {
		t.Fatalf("the cookie after logout: %d", res.Status)
	}
}

func TestStaffOpensRestateAsAdmin(t *testing.T) {
	t.Parallel()
	admin := signInStaff(t, staffdomain.RoleAdmin)
	c := openDashboard(t, admin, "restate", "restate.e2e.test")
	res := dashboardAuth(t, "restate.e2e.test", "GET", c.Value)
	if res.Status != 200 || res.Header.Get("X-Staff-Role") != "admin" {
		t.Fatalf("%d %v", res.Status, res.Header)
	}
	c = openDashboard(t, admin, "grafana", "grafana.e2e.test")
	if res = dashboardAuth(t, "grafana.e2e.test", "GET", c.Value); res.Header.Get("X-Staff-Role") != "Admin" {
		t.Fatalf("grafana: %d %v", res.Status, res.Header)
	}
}

func TestStaffDashboardTicketRefusals(t *testing.T) {
	t.Parallel()
	mod := signInStaff(t, staffdomain.RoleModerator)
	if res := staffCall(t, mod, "POST", "/v1/staff/dashboard-tickets", map[string]string{"app": "restate"}); res.Status != 403 || errorOf(t, res).Code != "staff_forbidden" {
		t.Fatalf("a moderator for restate: %d %s", res.Status, res.Body)
	}
	if res := staffCall(t, mod, "POST", "/v1/staff/dashboard-tickets", map[string]string{"app": "kibana"}); res.Status != 400 {
		t.Fatalf("an unknown app: %d %s", res.Status, res.Body)
	}
	admin := signInStaff(t, staffdomain.RoleAdmin)
	if res := staffCall(t, admin, "POST", "/v1/staff/dashboard-tickets", map[string]string{"app": "litellm"}); res.Status != 422 || errorOf(t, res).Code != "dashboard_not_configured" {
		t.Fatalf("a dashboard without a host: %d %s", res.Status, res.Body)
	}
	if res := staffCall(t, staffSession{}, "POST", "/v1/staff/dashboard-tickets", map[string]string{"app": "grafana"}); res.Status != 401 {
		t.Fatalf("no session: %d", res.Status)
	}
}

func TestStaffDashboardCallbackRefusals(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, staffdomain.RoleModerator)
	res := staffCall(t, s, "POST", "/v1/staff/dashboard-tickets", map[string]string{"app": "grafana"})
	tk := decode[dashboardTicket](t, res)
	path := "/_dashboard/callback?ticket=" + url.QueryEscape(tk.Ticket) + "&rd=" + url.QueryEscape("//evil.test/")

	if res = call(t, server, "GET", path, nil, [2]string{"Host", "kibana.e2e.test"}); res.Status != 403 {
		t.Fatalf("an unknown host: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "GET", path, nil, [2]string{"Host", "restate.e2e.test"}); res.Status != 401 {
		t.Fatalf("another dashboard's host: %d %s", res.Status, res.Body)
	}
	res = call(t, server, "GET", path, nil, [2]string{"Host", "grafana.e2e.test"})
	if res.Status != 302 || res.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %v", res.Status, res.Header)
	}
	res = call(t, server, "GET", path, nil, [2]string{"Host", "grafana.e2e.test"})
	if res.Status != 401 || errorOf(t, res).Code != "invalid_dashboard_ticket" || strings.Contains(res.Header.Get("Set-Cookie"), "staff_dashboard") {
		t.Fatalf("a used ticket: %d %v %s", res.Status, res.Header, res.Body)
	}
	if res = call(t, server, "GET", "/_dashboard/callback", nil, [2]string{"Host", "grafana.e2e.test"}); res.Status != 400 {
		t.Fatalf("no ticket: %d", res.Status)
	}
}
