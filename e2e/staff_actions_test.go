package e2e

import (
	"net/url"
	"testing"

	"github.com/shopnexus/server2/internal/staff/member"
)

func TestStaffActions(t *testing.T) {
	t.Parallel()
	admin := signInStaff(t, member.RoleAdmin)
	mod := signInStaff(t, member.RoleModerator)
	customer, _ := newAccount(t)
	if res := call(t, server, "POST", "/v1/staff/accounts/"+customer.Account.ID+"/suspend", map[string]string{"reason": "spam"}, staffBearer(mod)); res.Status != 200 {
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
	res := call(t, server, "GET", "/v1/staff/actions?"+q.Encode(), nil, staffBearer(admin))
	got := decode[actionList](t, res)
	if res.Status != 200 || len(got.Items) != 1 || got.Items[0].Action != "account.suspend" || got.Items[0].Actor.DisplayName != "E2E Staff" {
		t.Fatalf("by target: %d %s", res.Status, res.Body)
	}
	q = url.Values{"staff_id": {mod.ID}, "action": {"staff.login"}}
	if got := decode[actionList](t, call(t, server, "GET", "/v1/staff/actions?"+q.Encode(), nil, staffBearer(admin))); len(got.Items) != 1 {
		t.Fatalf("the moderator's sign-in: %+v", got)
	}
	if res := call(t, server, "GET", "/v1/staff/actions", nil, staffBearer(mod)); res.Status != 403 {
		t.Fatalf("a moderator reading the log: %d", res.Status)
	}
}
