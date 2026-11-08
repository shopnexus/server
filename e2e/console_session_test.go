package e2e

import (
	"testing"
	"time"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

func TestConsoleMeAndLogout(t *testing.T) {
	t.Parallel()
	s := signInMember(t, consoledomain.RoleAdmin)
	res := consoleCall(t, s, "GET", "/v1/console/me", nil)
	if res.Status != 200 || decode[struct {
		Role string `json:"role"`
	}](t, res).Role != "admin" {
		t.Fatalf("me: %d %s", res.Status, res.Body)
	}
	if got := consoleCall(t, consoleSession{}, "GET", "/v1/console/me", nil); got.Status != 401 {
		t.Fatalf("me without a cookie: %d", got.Status)
	}
	if got := consoleCall(t, consoleSession{}, "POST", "/v1/console/logout", nil); got.Status != 401 {
		t.Fatalf("logout without a cookie: %d", got.Status)
	}
	res = consoleCall(t, s, "POST", "/v1/console/logout", nil)
	if res.Status != 204 || sessionCookieOf(t, res).MaxAge >= 0 {
		t.Fatalf("logout: %d %v", res.Status, res.Header)
	}
	if got := consoleCall(t, s, "GET", "/v1/console/me", nil); got.Status != 401 {
		t.Fatalf("the cookie after logout: %d", got.Status)
	}
}

func TestConsoleChangePassword(t *testing.T) {
	t.Parallel()
	s := signInMember(t, consoledomain.RoleModerator)
	body := func(current, next string) map[string]string {
		return map[string]string{"current_password": current, "new_password": next}
	}
	if res := consoleCall(t, s, "PUT", "/v1/console/me/password", body(memberPassword, "short")); res.Status != 422 {
		t.Fatalf("weak: %d %s", res.Status, res.Body)
	}
	if res := consoleCall(t, s, "PUT", "/v1/console/me/password", body("wrong current", "another long password")); res.Status != 401 {
		t.Fatalf("wrong current: %d %s", res.Status, res.Body)
	}
	if res := consoleCall(t, s, "PUT", "/v1/console/me/password", body(memberPassword, "another long password")); res.Status != 204 {
		t.Fatalf("change: %d %s", res.Status, res.Body)
	}
	if res := consoleCall(t, s, "GET", "/v1/console/me", nil); res.Status != 401 {
		t.Fatalf("this session after the change: %d", res.Status)
	}
	code := consoledomain.Code(s.Secret, time.Now().Add(30*time.Second))
	if res := consoleLogin(t, map[string]string{"email": s.Email, "password": "another long password", "code": code}); res.Status != 200 {
		t.Fatalf("signing in with the new password: %d %s", res.Status, res.Body)
	}
}

// A customer token means nothing on the console API and a console cookie nothing
// on the customer API.
func TestCredentialsStayInTheirAPI(t *testing.T) {
	t.Parallel()
	customer, _ := newAccount(t)
	s := signInMember(t, consoledomain.RoleModerator)
	if res := consoleCall(t, consoleSession{}, "GET", "/v1/console/me", nil, bearer(customer.AccessToken)); res.Status != 401 {
		t.Fatalf("a customer token on the console API: %d", res.Status)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, [2]string{"Cookie", "console_session=" + s.Token}); res.Status != 401 {
		t.Fatalf("a console cookie on the customer API: %d", res.Status)
	}
}
