package e2e

import (
	"net/url"
	"testing"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

func TestConsoleActions(t *testing.T) {
	t.Parallel()
	admin := signInMember(t, consoledomain.RoleAdmin)
	mod := signInMember(t, consoledomain.RoleModerator)
	customer, _ := newAccount(t)
	expectStatus(t, consoleCall(t, mod, "POST", "/v1/console/accounts/"+customer.Account.ID+"/suspend", map[string]string{"reason": "spam"}), 200)
	type action struct {
		Action string `json:"action"`
		Actor  struct {
			Kind        string `json:"kind"`
			DisplayName string `json:"display_name"`
		} `json:"actor"`
	}
	q := url.Values{"target_id": {customer.Account.ID}}
	res := consoleCall(t, admin, "GET", "/v1/console/actions?"+q.Encode(), nil)
	got := decode[page[action]](t, res)
	if res.Status != 200 || len(got.Items) != 1 || got.Items[0].Action != "account.suspend" || got.Items[0].Actor.DisplayName != "E2E Member" {
		t.Fatalf("by target: %d %s", res.Status, res.Body)
	}
	q = url.Values{"member_id": {mod.ID}}
	if got := decode[page[action]](t, consoleCall(t, admin, "GET", "/v1/console/actions?"+q.Encode(), nil)); len(got.Items) != 1 {
		t.Fatalf("the moderator's actions: %+v", got)
	}
	expectStatus(t, consoleCall(t, mod, "GET", "/v1/console/actions", nil), 403) // a moderator reading the log
}
