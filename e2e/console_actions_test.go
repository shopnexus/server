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
	if res := consoleCall(t, mod, "POST", "/v1/console/accounts/"+customer.Account.ID+"/suspend", map[string]string{"reason": "spam"}); res.Status != 200 {
		t.Fatalf("suspend: %d %s", res.Status, res.Body)
	}
	type actionList struct {
		Items []struct {
			Action string `json:"action"`
			Actor  struct {
				Kind        string `json:"kind"`
				DisplayName string `json:"display_name"`
			} `json:"actor"`
		} `json:"items"`
	}
	q := url.Values{"target_id": {customer.Account.ID}}
	res := consoleCall(t, admin, "GET", "/v1/console/actions?"+q.Encode(), nil)
	got := decode[actionList](t, res)
	if res.Status != 200 || len(got.Items) != 1 || got.Items[0].Action != "account.suspend" || got.Items[0].Actor.DisplayName != "E2E Member" {
		t.Fatalf("by target: %d %s", res.Status, res.Body)
	}
	q = url.Values{"member_id": {mod.ID}}
	if got := decode[actionList](t, consoleCall(t, admin, "GET", "/v1/console/actions?"+q.Encode(), nil)); len(got.Items) != 1 {
		t.Fatalf("the moderator's actions: %+v", got)
	}
	if res := consoleCall(t, mod, "GET", "/v1/console/actions", nil); res.Status != 403 {
		t.Fatalf("a moderator reading the log: %d", res.Status)
	}
}
