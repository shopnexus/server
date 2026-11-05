package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/staff/domain"
	"github.com/shopnexus/server2/internal/staff/handler"
	"github.com/shopnexus/server2/internal/staff/service"
)

// dashboards opens ticket dt_good and knows two cookies: ds_viewer, granted,
// and ds_low, whose role is below the floor.
type dashboards struct{ service.Service }

func (dashboards) OpenDashboard(_ context.Context, _, ticket string) (service.DashboardOpened, error) {
	if ticket != "dt_good" {
		return service.DashboardOpened{}, domain.ErrInvalidDashboardTicket
	}
	return service.DashboardOpened{Token: "ds_new", TTL: 2*time.Hour + 500*time.Millisecond}, nil
}

func (dashboards) AuthorizeDashboard(_ context.Context, in service.DashboardCheck) (service.DashboardAccess, error) {
	switch in.Token {
	case "ds_viewer":
		return service.DashboardAccess{Granted: true, Email: "mod@example.com", Role: "Viewer"}, nil
	case "ds_low":
		return service.DashboardAccess{}, domain.ErrStaffForbidden
	default:
		return service.DashboardAccess{OpenURL: "https://console.test/dashboards/open?app=grafana&rd=" + url.QueryEscape(in.URI)}, nil
	}
}

func dashboardRouter() *httpx.Router {
	r := httpx.NewRouter()
	handler.New(dashboards{}, discard).Register(r)
	return r
}

func callback(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/_dashboard/callback?"+query, http.NoBody)
	r.Host = "grafana.test"
	w := httptest.NewRecorder()
	dashboardRouter().ServeHTTP(w, r)
	return w
}

// The callback sends the reader on only to a path of the same host.
func TestOpenDashboardRedirectsOnlyOnTheHost(t *testing.T) {
	t.Parallel()
	for rd, want := range map[string]string{
		"/d/abc?from=now-1h":  "/d/abc?from=now-1h",
		"/":                   "/",
		"":                    "/",
		"//evil.test/x":       "/",
		"https://evil.test/x": "/",
		"evil.test/x":         "/",
		"/\\evil.test/x":      "/",
		"\\/evil.test/x":      "/",
		"/\t/evil.test/x":     "/",
		"/\n/evil.test/x":     "/",
		"javascript:alert(1)": "/",
	} {
		w := callback(t, url.Values{"ticket": {"dt_good"}, "rd": {rd}}.Encode())
		if w.Code != http.StatusFound || w.Header().Get("Location") != want {
			t.Errorf("rd %q: %d %q, want %q", rd, w.Code, w.Header().Get("Location"), want)
		}
	}
}

func TestOpenDashboardSetsAHostCookie(t *testing.T) {
	t.Parallel()
	w := callback(t, "ticket=dt_good")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("%v", w.Header())
	}
	c := cookies[0]
	if c.Name != "staff_dashboard" || c.Value != "ds_new" || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode ||
		c.Path != "/" || c.Domain != "" || c.MaxAge != 7201 {
		t.Fatalf("%+v", c)
	}
	w = callback(t, "ticket=dt_used")
	if w.Code != http.StatusUnauthorized || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
		t.Fatalf("a spent ticket: %d %v", w.Code, w.Header())
	}
}

func TestAuthorizeDashboard(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cookie, method string
		status               int
		location             string
	}{
		{"granted", "ds_viewer", "GET", 200, ""},
		{"granted for a POST", "ds_viewer", "POST", 200, ""},
		{"no cookie, GET", "", "GET", 302, "https://console.test/dashboards/open?app=grafana&rd=%2Fd%2Fx%3Fa%3D1"},
		{"no cookie, no method", "", "", 302, "https://console.test/dashboards/open?app=grafana&rd=%2Fd%2Fx%3Fa%3D1"},
		{"an unknown cookie, POST", "ds_unknown", "POST", 401, ""},
		{"no cookie, DELETE", "", "DELETE", 401, ""},
		{"below the floor", "ds_low", "GET", 403, ""},
	}
	for _, c := range cases {
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/v1/staff/dashboard-auth", http.NoBody)
		r.Header.Set("X-Forwarded-Host", "grafana.test")
		r.Header.Set("X-Forwarded-Uri", "/d/x?a=1")
		if c.method != "" {
			r.Header.Set("X-Forwarded-Method", c.method)
		}
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "staff_dashboard", Value: c.cookie})
		}
		w := httptest.NewRecorder()
		dashboardRouter().ServeHTTP(w, r)
		if w.Code != c.status || w.Header().Get("Location") != c.location {
			t.Errorf("%s: %d %q", c.name, w.Code, w.Header().Get("Location"))
		}
		if granted := w.Header().Get("X-Staff-Email") == "mod@example.com" && w.Header().Get("X-Staff-Role") == "Viewer"; granted != (c.status == 200) {
			t.Errorf("%s: headers %v", c.name, w.Header())
		}
	}
}
