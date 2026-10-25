package e2e

import (
	"testing"
	"time"

	staffdomain "github.com/shopnexus/server2/internal/staff/domain"
)

func TestStaffMeAndLogout(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, staffdomain.RoleAdmin)
	res := staffCall(t, s, "GET", "/v1/staff/me", nil)
	if res.Status != 200 || decode[struct {
		Role string `json:"role"`
	}](t, res).Role != "admin" {
		t.Fatalf("me: %d %s", res.Status, res.Body)
	}
	if got := staffCall(t, staffSession{}, "GET", "/v1/staff/me", nil); got.Status != 401 {
		t.Fatalf("me without a cookie: %d", got.Status)
	}
	if got := staffCall(t, staffSession{}, "POST", "/v1/staff/logout", nil); got.Status != 401 {
		t.Fatalf("logout without a cookie: %d", got.Status)
	}
	res = staffCall(t, s, "POST", "/v1/staff/logout", nil)
	if res.Status != 204 || sessionCookieOf(t, res).MaxAge >= 0 {
		t.Fatalf("logout: %d %v", res.Status, res.Header)
	}
	if got := staffCall(t, s, "GET", "/v1/staff/me", nil); got.Status != 401 {
		t.Fatalf("the cookie after logout: %d", got.Status)
	}
}

func TestStaffChangePassword(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, staffdomain.RoleModerator)
	body := func(current, next string) map[string]string {
		return map[string]string{"current_password": current, "new_password": next}
	}
	if res := staffCall(t, s, "PUT", "/v1/staff/me/password", body(staffPassword, "short")); res.Status != 422 {
		t.Fatalf("weak: %d %s", res.Status, res.Body)
	}
	if res := staffCall(t, s, "PUT", "/v1/staff/me/password", body("wrong current", "another long password")); res.Status != 401 {
		t.Fatalf("wrong current: %d %s", res.Status, res.Body)
	}
	if res := staffCall(t, s, "PUT", "/v1/staff/me/password", body(staffPassword, "another long password")); res.Status != 204 {
		t.Fatalf("change: %d %s", res.Status, res.Body)
	}
	if res := staffCall(t, s, "GET", "/v1/staff/me", nil); res.Status != 401 {
		t.Fatalf("this session after the change: %d", res.Status)
	}
	code := staffdomain.Code(s.Secret, time.Now().Add(30*time.Second))
	if res := staffLogin(t, map[string]string{"email": s.Email, "password": "another long password", "code": code}); res.Status != 200 {
		t.Fatalf("signing in with the new password: %d %s", res.Status, res.Body)
	}
}

// A customer token means nothing on the staff API and a staff cookie nothing
// on the customer API.
func TestCredentialsStayInTheirAPI(t *testing.T) {
	t.Parallel()
	customer, _ := newAccount(t)
	s := signInStaff(t, staffdomain.RoleModerator)
	if res := staffCall(t, staffSession{}, "GET", "/v1/staff/me", nil, bearer(customer.AccessToken)); res.Status != 401 {
		t.Fatalf("a customer token on the staff API: %d", res.Status)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, [2]string{"Cookie", "staff_session=" + s.Token}); res.Status != 401 {
		t.Fatalf("a staff cookie on the customer API: %d", res.Status)
	}
}
