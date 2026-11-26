package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	consoledomain "github.com/shopnexus/server/internal/console/domain"
)

type dashboardTicket struct {
	Ticket      string `json:"ticket"`
	CallbackURL string `json:"callback_url"`
}

// openDashboard takes s from the console to app's host the way a browser
// goes: a ticket, then the callback on the dashboard's host. It returns the
// console_dashboard cookie.
func openDashboard(t *testing.T, s consoleSession, app, host string) *http.Cookie {
	t.Helper()
	res := consoleCall(t, s, "POST", "/v1/console/dashboard-tickets", map[string]string{"app": app})
	tk := expect[dashboardTicket](t, res, 200)
	if tk.CallbackURL != "https://"+host+"/_dashboard/callback?ticket="+url.QueryEscape(tk.Ticket) {
		t.Fatalf("callback_url %q", tk.CallbackURL)
	}
	res = call(t, "GET", "/_dashboard/callback?ticket="+url.QueryEscape(tk.Ticket)+"&rd=%2Fd%2Fabc%3Ffrom%3Dnow-1h", nil, [2]string{"Host", host})
	if res.Status != 302 || res.Header.Get("Location") != "/d/abc?from=now-1h" {
		t.Fatalf("callback: %d %v %s", res.Status, res.Header, res.Body)
	}
	for _, c := range (&http.Response{Header: res.Header}).Cookies() {
		if c.Name == "console_dashboard" {
			return c
		}
	}
	t.Fatalf("no console_dashboard cookie in %v", res.Header)
	return nil
}

// dashboardAuth calls the forwardAuth check as Traefik does for a request
// to host.
func dashboardAuth(t *testing.T, host, method, cookie string) Response {
	t.Helper()
	h := [][2]string{{"X-Forwarded-Host", host}, {"X-Forwarded-Uri", "/d/abc?x=1"}, {"X-Forwarded-Method", method}}
	if cookie != "" {
		h = append(h, [2]string{"Cookie", "console_dashboard=" + cookie})
	}
	return call(t, "GET", "/v1/console/dashboard-auth", nil, h...)
}

// UC-MOD-10 A3.
func TestConsoleOpensGrafana(t *testing.T) {
	t.Parallel()
	s := signInMember(t, consoledomain.RoleModerator)
	c := openDashboard(t, s, "grafana", "grafana.e2e.test")
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" ||
		c.MaxAge <= 0 || c.MaxAge > int(consoledomain.SessionTTL.Seconds()) {
		t.Fatalf("cookie %+v", c)
	}

	res := dashboardAuth(t, "grafana.e2e.test:443", "POST", c.Value)
	if res.Status != 200 || res.Header.Get("X-Console-Email") != s.Email || res.Header.Get("X-Console-Role") != "Viewer" {
		t.Fatalf("with the cookie: %d %v", res.Status, res.Header)
	}
	res = dashboardAuth(t, "grafana.e2e.test", "GET", "")
	if res.Status != 302 || res.Header.Get("Location") != "http://console.e2e/dashboards/open?app=grafana&rd=%2Fd%2Fabc%3Fx%3D1" {
		t.Fatalf("no cookie, GET: %d %v", res.Status, res.Header)
	}
	expectStatus(t, dashboardAuth(t, "grafana.e2e.test", "POST", ""), 401)     // no cookie, POST
	expectStatus(t, dashboardAuth(t, "restate.e2e.test", "GET", c.Value), 302) // grafana's cookie on restate
	if res = dashboardAuth(t, "kibana.e2e.test", "GET", c.Value); res.Status != 403 || errorOf(t, res).Code != "dashboard_host_unknown" {
		t.Fatalf("an unknown host: %d %s", res.Status, res.Body)
	}

	expectStatus(t, consoleCall(t, s, "POST", "/v1/console/logout", nil), 204)
	expectStatus(t, dashboardAuth(t, "grafana.e2e.test", "GET", c.Value), 302) // the cookie after logout
}

func TestConsoleOpensRestateAsAdmin(t *testing.T) {
	t.Parallel()
	admin := signInMember(t, consoledomain.RoleAdmin)
	c := openDashboard(t, admin, "restate", "restate.e2e.test")
	res := dashboardAuth(t, "restate.e2e.test", "GET", c.Value)
	if res.Status != 200 || res.Header.Get("X-Console-Role") != "admin" {
		t.Fatalf("%d %v", res.Status, res.Header)
	}
	c = openDashboard(t, admin, "grafana", "grafana.e2e.test")
	if res = dashboardAuth(t, "grafana.e2e.test", "GET", c.Value); res.Header.Get("X-Console-Role") != "Admin" {
		t.Fatalf("grafana: %d %v", res.Status, res.Header)
	}
}

// UC-MOD-10 E6.
func TestConsoleDashboardTicketRefusals(t *testing.T) {
	t.Parallel()
	mod := signInMember(t, consoledomain.RoleModerator)
	if res := consoleCall(t, mod, "POST", "/v1/console/dashboard-tickets", map[string]string{"app": "restate"}); res.Status != 403 || errorOf(t, res).Code != "console_forbidden" {
		t.Fatalf("a moderator for restate: %d %s", res.Status, res.Body)
	}
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/dashboard-tickets", map[string]string{"app": "kibana"}), 400) // an unknown app
	admin := signInMember(t, consoledomain.RoleAdmin)
	if res := consoleCall(t, admin, "POST", "/v1/console/dashboard-tickets", map[string]string{"app": "bifrost"}); res.Status != 422 || errorOf(t, res).Code != "dashboard_not_configured" {
		t.Fatalf("a dashboard without a host: %d %s", res.Status, res.Body)
	}
	expectStatus(t, consoleCall(t, consoleSession{}, "POST", "/v1/console/dashboard-tickets", map[string]string{"app": "grafana"}), 401)
}

// UC-MOD-10 E7.
func TestConsoleDashboardCallbackRefusals(t *testing.T) {
	t.Parallel()
	s := signInMember(t, consoledomain.RoleModerator)
	res := consoleCall(t, s, "POST", "/v1/console/dashboard-tickets", map[string]string{"app": "grafana"})
	tk := decode[dashboardTicket](t, res)
	path := "/_dashboard/callback?ticket=" + url.QueryEscape(tk.Ticket) + "&rd=" + url.QueryEscape("//evil.test/")

	expectStatus(t, call(t, "GET", path, nil, [2]string{"Host", "kibana.e2e.test"}), 403)  // an unknown host
	expectStatus(t, call(t, "GET", path, nil, [2]string{"Host", "restate.e2e.test"}), 401) // another dashboard's host
	res = call(t, "GET", path, nil, [2]string{"Host", "grafana.e2e.test"})
	if res.Status != 302 || res.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %v", res.Status, res.Header)
	}
	res = call(t, "GET", path, nil, [2]string{"Host", "grafana.e2e.test"})
	if res.Status != 401 || errorOf(t, res).Code != "invalid_dashboard_ticket" || strings.Contains(res.Header.Get("Set-Cookie"), "console_dashboard") {
		t.Fatalf("a used ticket: %d %v %s", res.Status, res.Header, res.Body)
	}
	expectStatus(t, call(t, "GET", "/_dashboard/callback", nil, [2]string{"Host", "grafana.e2e.test"}), 400) // no ticket
}
