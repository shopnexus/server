package e2e

import (
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/staff/member"
)

var memberOps = []string{"disable", "enable", "unlock", "reset-password", "reset-totp"}

func TestStaffManagesModerators(t *testing.T) {
	t.Parallel()
	admin := signInStaff(t, member.RoleAdmin)
	email := newStaffEmail()
	res := call(t, server, "POST", "/v1/staff/members", map[string]string{"email": email, "display_name": "New Mod"}, staffBearer(admin))
	created := decode[struct {
		Member struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"member"`
		Temporary string `json:"temporary_password"`
	}](t, res)
	if res.Status != 201 || created.Member.Role != "moderator" || len(created.Temporary) != 16 {
		t.Fatalf("create: %d %s", res.Status, res.Body)
	}
	if got := call(t, server, "POST", "/v1/staff/members", map[string]string{"email": email, "display_name": "Again"}, staffBearer(admin)); got.Status != 409 {
		t.Fatalf("the same email: %d %s", got.Status, got.Body)
	}
	if got := call(t, server, "POST", "/v1/staff/members", map[string]string{"email": "nope", "display_name": "Bad"}, staffBearer(admin)); got.Status != 400 {
		t.Fatalf("a bad email: %d %s", got.Status, got.Body)
	}
	if got := call(t, server, "GET", "/v1/staff/members?limit=100", nil, staffBearer(admin)); got.Status != 200 {
		t.Fatalf("list: %d %s", got.Status, got.Body)
	}
	if got := call(t, server, "GET", "/v1/staff/members", nil); got.Status != 401 {
		t.Fatalf("list without a token: %d", got.Status)
	}

	mod := signInStaff(t, member.RoleModerator)
	path := "/v1/staff/members/" + mod.ID + "/"
	if got := call(t, server, "POST", path+"disable", nil, staffBearer(admin)); got.Status != 200 {
		t.Fatalf("disable: %d %s", got.Status, got.Body)
	}
	if got := call(t, server, "GET", "/v1/staff/me", nil, staffBearer(mod)); got.Status != 401 {
		t.Fatalf("the disabled moderator's token: %d", got.Status)
	}
	if got := call(t, server, "POST", path+"enable", nil, staffBearer(admin)); got.Status != 200 {
		t.Fatalf("enable: %d %s", got.Status, got.Body)
	}
	for range 5 {
		staffLogin(t, mod.Email, "wrong password")
	}
	if got := staffLogin(t, mod.Email, mod.Password); got.Status != 429 {
		t.Fatalf("locked: %d", got.Status)
	}
	if got := call(t, server, "POST", path+"unlock", nil, staffBearer(admin)); got.Status != 200 {
		t.Fatalf("unlock: %d %s", got.Status, got.Body)
	}
	if got := staffLogin(t, mod.Email, mod.Password); got.Status != 200 {
		t.Fatalf("after unlock: %d %s", got.Status, got.Body)
	}
	res = call(t, server, "POST", path+"reset-password", nil, staffBearer(admin))
	if res.Status != 200 || len(decode[struct {
		Temporary string `json:"temporary_password"`
	}](t, res).Temporary) != 16 {
		t.Fatalf("reset password: %d %s", res.Status, res.Body)
	}
	res = call(t, server, "POST", path+"reset-totp", nil, staffBearer(admin))
	if res.Status != 200 || decode[struct {
		Enrolled bool `json:"totp_enrolled"`
	}](t, res).Enrolled {
		t.Fatalf("reset totp: %d %s", res.Status, res.Body)
	}
}

func TestStaffMemberOpsForbidden(t *testing.T) {
	t.Parallel()
	admin := signInStaff(t, member.RoleAdmin)
	mod := signInStaff(t, member.RoleModerator)
	otherAdmin, _, _ := newStaff(t, member.RoleAdmin)
	if got := call(t, server, "GET", "/v1/staff/members", nil, staffBearer(mod)); got.Status != 403 || errorOf(t, got).Code != "staff_forbidden" {
		t.Fatalf("a moderator listing: %d %s", got.Status, got.Body)
	}
	if got := call(t, server, "POST", "/v1/staff/members", map[string]string{"email": newStaffEmail(), "display_name": "X"}, staffBearer(mod)); got.Status != 403 {
		t.Fatalf("a moderator creating: %d", got.Status)
	}
	for _, op := range memberOps {
		path := "/v1/staff/members/" + otherAdmin + "/" + op
		if got := call(t, server, "POST", path, nil, staffBearer(mod)); got.Status != 403 || errorOf(t, got).Code != "staff_forbidden" {
			t.Errorf("%s by a moderator: %d %s", op, got.Status, got.Body)
		}
		if got := call(t, server, "POST", path, nil, staffBearer(admin)); got.Status != 403 || errorOf(t, got).Code != "admin_managed_by_cli" {
			t.Errorf("%s on an admin: %d %s", op, got.Status, got.Body)
		}
		if got := call(t, server, "POST", "/v1/staff/members/"+uuid.NewV7().String()+"/"+op, nil, staffBearer(admin)); got.Status != 404 {
			t.Errorf("%s on an unknown id: %d %s", op, got.Status, got.Body)
		}
	}
}
