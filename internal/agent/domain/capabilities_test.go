package domain_test

import (
	"slices"
	"testing"

	"github.com/shopnexus/server/api"
	"github.com/shopnexus/server/internal/agent/domain"
)

// An admin's operation is in an admin's turn only, and only when the agent
// holds its capability. UC-MOD-13 E3.
func TestGrantsByRole(t *testing.T) {
	t.Parallel()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	c, err := domain.NewCatalog(doc)
	if err != nil {
		t.Fatal(err)
	}
	names := func(ops []domain.Operation) []string {
		out := make([]string, 0, len(ops))
		for _, o := range ops {
			out = append(out, o.Name)
		}
		return out
	}
	health, ok := c.Operation("GET", "/v1/console/monitoring/health")
	if !ok || health.Role != domain.RoleAdmin {
		t.Fatalf("the health operation: %+v", health)
	}
	caps := []string{"monitoring.read", "tickets.read"}
	admin, moderator := names(c.Granted(caps, "admin")), names(c.Granted(caps, "moderator"))
	if !slices.Contains(admin, "getSystemHealth") || !slices.Contains(admin, "listTickets") {
		t.Errorf("an admin's turn: %v", admin)
	}
	if slices.Contains(moderator, "getSystemHealth") || !slices.Contains(moderator, "listTickets") {
		t.Errorf("a moderator's turn: %v", moderator)
	}
	if domain.Grants([]string{"tickets.read"}, "admin", health) {
		t.Error("granted without the capability")
	}
	if all := c.Operations(); len(all) <= len(admin) || slices.ContainsFunc(all, func(o domain.Operation) bool { return o.Name == domain.OperationsName }) {
		t.Errorf("every operation: %d", len(all))
	}
}
