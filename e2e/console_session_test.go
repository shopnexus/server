package e2e

import (
	"testing"
	"time"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

func TestConsoleMeAndLogout(t *testing.T) {
	t.Parallel()
	s := signInMember(t, consoledomain.RoleAdmin)
	if me := expect[struct {
		Role string `json:"role"`
	}](t, consoleCall(t, s, "GET", "/v1/console/me", nil), 200); me.Role != "admin" {
		t.Fatalf("me: %+v", me)
	}
	expectStatus(t, consoleCall(t, consoleSession{}, "GET", "/v1/console/me", nil), 401)
	expectStatus(t, consoleCall(t, consoleSession{}, "POST", "/v1/console/logout", nil), 401)
	res := consoleCall(t, s, "POST", "/v1/console/logout", nil)
	if res.Status != 204 || sessionCookieOf(t, res).MaxAge >= 0 {
		t.Fatalf("logout: %d %v", res.Status, res.Header)
	}
	expectStatus(t, consoleCall(t, s, "GET", "/v1/console/me", nil), 401) // the cookie after logout
}

func TestConsoleChangePassword(t *testing.T) {
	t.Parallel()
	s := signInMember(t, consoledomain.RoleModerator)
	body := func(current, next string) map[string]string {
		return map[string]string{"current_password": current, "new_password": next}
	}
	expectStatus(t, consoleCall(t, s, "PUT", "/v1/console/me/password", body(memberPassword, "short")), 422)
	expectStatus(t, consoleCall(t, s, "PUT", "/v1/console/me/password", body("wrong current", "another long password")), 401)
	expectStatus(t, consoleCall(t, s, "PUT", "/v1/console/me/password", body(memberPassword, "another long password")), 204)
	expectStatus(t, consoleCall(t, s, "GET", "/v1/console/me", nil), 401) // this session after the change
	code := consoledomain.Code(s.Secret, time.Now().Add(30*time.Second))
	expectStatus(t, consoleLogin(t, map[string]string{"email": s.Email, "password": "another long password", "code": code}), 200) // signing in with the new password
}

// A customer token means nothing on the console API and a console cookie nothing
// on the customer API.
func TestCredentialsStayInTheirAPI(t *testing.T) {
	t.Parallel()
	customer, _ := newAccount(t)
	s := signInMember(t, consoledomain.RoleModerator)
	expectStatus(t, consoleCall(t, consoleSession{}, "GET", "/v1/console/me", nil, bearer(customer.AccessToken)), 401) // a customer token on the console API
	expectStatus(t, call(t, "GET", "/v1/accounts/me", nil, [2]string{"Cookie", "console_session=" + s.Token}), 401)    // a console cookie on the customer API
}
