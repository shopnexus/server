package domain_test

import (
	"testing"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/staff/domain"
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
