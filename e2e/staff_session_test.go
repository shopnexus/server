package e2e

import (
	"testing"

	"github.com/shopnexus/server2/internal/staff/member"
)

func TestStaffRefresh(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, member.RoleModerator)
	res := refreshWith(t, s.Refresh, csrf)
	if res.Status != 200 {
		t.Fatalf("refresh: %d %s", res.Status, res.Body)
	}
	next := refreshCookieOf(t, res).Value
	if next == s.Refresh {
		t.Fatal("the cookie did not rotate")
	}
	if res := refreshWith(t, s.Refresh, csrf); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
		t.Fatalf("the old cookie again: %d %s", res.Status, res.Body)
	}
	if res := refreshWith(t, next, csrf); res.Status != 401 {
		t.Fatalf("the newer cookie after a replay: %d %s", res.Status, res.Body)
	}
}

func TestStaffRefreshRefusals(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, member.RoleModerator)
	if res := refreshWith(t, s.Refresh); res.Status != 400 {
		t.Fatalf("without X-Requested-With: %d %s", res.Status, res.Body)
	}
	if res := refreshWith(t, "rt_garbage", csrf); res.Status != 401 || errorOf(t, res).Code != "invalid_refresh_token" {
		t.Fatalf("a garbage cookie: %d %s", res.Status, res.Body)
	}
}

func TestStaffMeAndLogout(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, member.RoleAdmin)
	res := call(t, server, "GET", "/v1/staff/me", nil, staffBearer(s))
	if res.Status != 200 || decode[struct {
		Role string `json:"role"`
	}](t, res).Role != "admin" {
		t.Fatalf("me: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/staff/me", nil); res.Status != 401 {
		t.Fatalf("me without a token: %d", res.Status)
	}
	if res := call(t, server, "POST", "/v1/staff/session/logout", nil); res.Status != 401 {
		t.Fatalf("logout without a token: %d", res.Status)
	}
	if res := call(t, server, "POST", "/v1/staff/session/logout", nil, staffBearer(s)); res.Status != 204 {
		t.Fatalf("logout: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/staff/me", nil, staffBearer(s)); res.Status != 401 {
		t.Fatalf("the access token after logout: %d", res.Status)
	}
}

func TestStaffChangePassword(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, member.RoleModerator)
	body := func(current, next string) map[string]string {
		return map[string]string{"current_password": current, "new_password": next}
	}
	if res := call(t, server, "PUT", "/v1/staff/me/password", body(s.Password, "short"), staffBearer(s)); res.Status != 422 {
		t.Fatalf("weak: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "PUT", "/v1/staff/me/password", body("wrong current", "another long password"), staffBearer(s)); res.Status != 401 {
		t.Fatalf("wrong current: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "PUT", "/v1/staff/me/password", body(s.Password, "another long password"), staffBearer(s)); res.Status != 204 {
		t.Fatalf("change: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/staff/me", nil, staffBearer(s)); res.Status != 200 {
		t.Fatalf("this session after the change: %d", res.Status)
	}
}

// A customer token fails on the staff API and a staff token on the customer
// API, because each verifier checks its audience.
func TestTokensStayInTheirAPI(t *testing.T) {
	t.Parallel()
	customer, _ := newAccount(t)
	s := signInStaff(t, member.RoleModerator)
	if res := call(t, server, "GET", "/v1/staff/me", nil, bearer(customer.AccessToken)); res.Status != 401 {
		t.Fatalf("a customer token on the staff API: %d", res.Status)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, staffBearer(s)); res.Status != 401 {
		t.Fatalf("a staff token on the customer API: %d", res.Status)
	}
}
