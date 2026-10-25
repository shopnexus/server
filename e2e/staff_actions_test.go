package e2e

import (
	"net/url"
	"testing"

	staffdomain "github.com/shopnexus/server2/internal/staff/domain"
)

func TestStaffActions(t *testing.T) {
	t.Parallel()
	admin := signInStaff(t, staffdomain.RoleAdmin)
	mod := signInStaff(t, staffdomain.RoleModerator)
	customer, _ := newAccount(t)
	if res := staffCall(t, mod, "POST", "/v1/staff/accounts/"+customer.Account.ID+"/suspend", map[string]string{"reason": "spam"}); res.Status != 200 {
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
	res := staffCall(t, admin, "GET", "/v1/staff/actions?"+q.Encode(), nil)
	got := decode[actionList](t, res)
	if res.Status != 200 || len(got.Items) != 1 || got.Items[0].Action != "account.suspend" || got.Items[0].Actor.DisplayName != "E2E Staff" {
		t.Fatalf("by target: %d %s", res.Status, res.Body)
	}
	q = url.Values{"staff_id": {mod.ID}}
	if got := decode[actionList](t, staffCall(t, admin, "GET", "/v1/staff/actions?"+q.Encode(), nil)); len(got.Items) != 1 {
		t.Fatalf("the moderator's actions: %+v", got)
	}
	if res := staffCall(t, mod, "GET", "/v1/staff/actions", nil); res.Status != 403 {
		t.Fatalf("a moderator reading the log: %d", res.Status)
	}
}
