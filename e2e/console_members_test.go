package e2e

import (
	"testing"
	"uuid"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

var memberOps = []string{"disable", "enable", "unlock", "reset-password", "reset-totp"}

func TestConsoleManagesModerators(t *testing.T) {
	t.Parallel()
	admin := signInMember(t, consoledomain.RoleAdmin)
	email := newMemberEmail()
	res := consoleCall(t, admin, "POST", "/v1/console/members", map[string]string{"email": email, "display_name": "New Mod"})
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
	if got := consoleCall(t, admin, "POST", "/v1/console/members", map[string]string{"email": email, "display_name": "Again"}); got.Status != 409 {
		t.Fatalf("the same email: %d %s", got.Status, got.Body)
	}
	if got := consoleCall(t, admin, "POST", "/v1/console/members", map[string]string{"email": "nope", "display_name": "Bad"}); got.Status != 400 {
		t.Fatalf("a bad email: %d %s", got.Status, got.Body)
	}
	if got := consoleCall(t, admin, "GET", "/v1/console/members?limit=100", nil); got.Status != 200 {
		t.Fatalf("list: %d %s", got.Status, got.Body)
	}
	if got := consoleCall(t, consoleSession{}, "GET", "/v1/console/members", nil); got.Status != 401 {
		t.Fatalf("list without a cookie: %d", got.Status)
	}

	mod := signInMember(t, consoledomain.RoleModerator)
	path := "/v1/console/members/" + mod.ID + "/"
	if got := consoleCall(t, admin, "POST", path+"disable", nil); got.Status != 200 {
		t.Fatalf("disable: %d %s", got.Status, got.Body)
	}
	if got := consoleCall(t, mod, "GET", "/v1/console/me", nil); got.Status != 401 {
		t.Fatalf("the disabled moderator's cookie: %d", got.Status)
	}
	if got := consoleCall(t, admin, "POST", path+"enable", nil); got.Status != 200 {
		t.Fatalf("enable: %d %s", got.Status, got.Body)
	}
	for range 5 {
		consoleLogin(t, map[string]string{"email": mod.Email, "password": "wrong password"})
	}
	if got := consoleLogin(t, map[string]string{"email": mod.Email, "password": memberPassword}); got.Status != 429 {
		t.Fatalf("locked: %d", got.Status)
	}
	if got := consoleCall(t, admin, "POST", path+"unlock", nil); got.Status != 200 {
		t.Fatalf("unlock: %d %s", got.Status, got.Body)
	}
	if got := consoleLogin(t, map[string]string{"email": mod.Email, "password": memberPassword}); errorOf(t, got).Code != "totp_required" {
		t.Fatalf("after unlock: %d %s", got.Status, got.Body)
	}
	res = consoleCall(t, admin, "POST", path+"reset-password", nil)
	if res.Status != 200 || len(decode[struct {
		Temporary string `json:"temporary_password"`
	}](t, res).Temporary) != 16 {
		t.Fatalf("reset password: %d %s", res.Status, res.Body)
	}
	res = consoleCall(t, admin, "POST", path+"reset-totp", nil)
	if res.Status != 200 || decode[struct {
		Enrolled bool `json:"totp_enrolled"`
	}](t, res).Enrolled {
		t.Fatalf("reset totp: %d %s", res.Status, res.Body)
	}
}

func TestConsoleMemberOpsForbidden(t *testing.T) {
	t.Parallel()
	admin := signInMember(t, consoledomain.RoleAdmin)
	mod := signInMember(t, consoledomain.RoleModerator)
	otherAdmin, _, _ := newMember(t, consoledomain.RoleAdmin)
	if got := consoleCall(t, mod, "GET", "/v1/console/members", nil); got.Status != 403 || errorOf(t, got).Code != "console_forbidden" {
		t.Fatalf("a moderator listing: %d %s", got.Status, got.Body)
	}
	if got := consoleCall(t, mod, "POST", "/v1/console/members", map[string]string{"email": newMemberEmail(), "display_name": "X"}); got.Status != 403 {
		t.Fatalf("a moderator creating: %d", got.Status)
	}
	for _, op := range memberOps {
		path := "/v1/console/members/" + otherAdmin + "/" + op
		if got := consoleCall(t, mod, "POST", path, nil); got.Status != 403 || errorOf(t, got).Code != "console_forbidden" {
			t.Errorf("%s by a moderator: %d %s", op, got.Status, got.Body)
		}
		if got := consoleCall(t, admin, "POST", path, nil); got.Status != 403 || errorOf(t, got).Code != "admin_managed_by_cli" {
			t.Errorf("%s on an admin: %d %s", op, got.Status, got.Body)
		}
		if got := consoleCall(t, admin, "POST", "/v1/console/members/"+uuid.NewV7().String()+"/"+op, nil); got.Status != 404 {
			t.Errorf("%s on an unknown id: %d %s", op, got.Status, got.Body)
		}
	}
}
