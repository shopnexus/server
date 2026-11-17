package domain_test

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/platform/config"
)

// Every dashboard DASHBOARD_HOSTS may name has a floor.
func TestEveryDashboardHasAFloor(t *testing.T) {
	t.Parallel()
	for _, name := range config.DashboardApps {
		if _, ok := domain.App(name).Floor(); !ok {
			t.Errorf("%s has no floor", name)
		}
	}
	if _, ok := domain.App("kibana").Floor(); ok {
		t.Error("an unknown app has a floor")
	}
	if f, _ := domain.AppGrafana.Floor(); f != domain.RoleModerator {
		t.Errorf("grafana: %s", f)
	}
	for _, a := range []domain.App{domain.AppRestate, domain.AppLiteLLM, domain.AppS3, domain.AppArgoCD} {
		if f, _ := a.Floor(); f != domain.RoleAdmin {
			t.Errorf("%s: %s", a, f)
		}
	}
}

func TestRoleFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		app  domain.App
		role domain.Role
		want string
	}{
		{domain.AppGrafana, domain.RoleAdmin, "Admin"},
		{domain.AppGrafana, domain.RoleModerator, "Viewer"},
		{domain.AppRestate, domain.RoleAdmin, "admin"},
		{domain.AppS3, domain.RoleModerator, "moderator"},
	}
	for _, c := range cases {
		if got := c.app.RoleFor(c.role); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.app, c.role, got, c.want)
		}
	}
}

// The callback carries the keys in the fragment and opens a page of the
// console, or its start page for any other path.
func TestStoreConsoleCallback(t *testing.T) {
	t.Parallel()
	c := domain.StoreCredentials{
		AccessKey: "AK", SecretKey: "S+K/=", SessionToken: "tok&en",
		Expiration: time.Date(2026, 11, 17, 10, 0, 0, 0, time.FixedZone("ICT", 7*3600)),
	}
	for rd, page := range map[string]string{
		"/rustfs/console/browser/shopnexus/": "/browser/shopnexus/",
		"/rustfs/console/":                   "/",
		"/rustfs/console/auth/login/":        "/",
		"/rustfs/consoleevil":                "/",
		"/":                                  "/",
	} {
		got := domain.StoreConsoleCallback(c, rd)
		path, frag, ok := strings.Cut(got, "#")
		if !ok || path != "/rustfs/console/auth/oidc-callback/" {
			t.Fatalf("rd %q: %q", rd, got)
		}
		f, err := url.ParseQuery(frag)
		if err != nil {
			t.Fatal(err)
		}
		want := url.Values{
			"accessKey": {"AK"}, "secretKey": {"S+K/="}, "sessionToken": {"tok&en"},
			"expiration": {"2026-11-17T03:00:00Z"}, "redirect": {page},
		}
		if !reflect.DeepEqual(f, want) {
			t.Errorf("rd %q: %v", rd, f)
		}
	}
}
