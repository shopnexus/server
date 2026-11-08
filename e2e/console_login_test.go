package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

func TestConsoleFirstSignIn(t *testing.T) {
	t.Parallel()
	_, email, temp := newMember(t, consoledomain.RoleModerator)
	enroll := func(password string) Response {
		return consoleCall(t, consoleSession{}, "POST", "/v1/console/login/totp-enrollment", map[string]string{"email": email, "password": password})
	}
	steps := []struct {
		name string
		res  Response
		want string
	}{
		{"temporary password", consoleLogin(t, map[string]string{"email": email, "password": temp}), "password_change_required"},
		{"enrollment first", enroll(temp), "password_change_required"},
		{"weak password", consoleLogin(t, map[string]string{"email": email, "password": temp, "new_password": "short"}), "password_too_weak"},
		{"new password", consoleLogin(t, map[string]string{"email": email, "password": temp, "new_password": memberPassword}), "totp_enrollment_required"},
		{"temporary again", enroll(temp), "invalid_credentials"},
	}
	for _, s := range steps {
		if got := errorOf(t, s.res).Code; got != s.want {
			t.Fatalf("%s: %d %s, want %s", s.name, s.res.Status, s.res.Body, s.want)
		}
	}
	res := enroll(memberPassword)
	enr := decode[struct {
		Secret string `json:"secret"`
		URI    string `json:"otpauth_uri"`
	}](t, res)
	if res.Status != 200 || enr.Secret == "" || enr.URI == "" {
		t.Fatalf("enrollment: %d %s", res.Status, res.Body)
	}
	if res = consoleLogin(t, map[string]string{"email": email, "password": memberPassword}); errorOf(t, res).Code != "totp_required" {
		t.Fatalf("no code: %d %s", res.Status, res.Body)
	}
	code := consoledomain.Code(decodeBase32(t, enr.Secret), time.Now())
	res = consoleLogin(t, map[string]string{"email": email, "password": memberPassword, "code": code}, [2]string{"User-Agent", "e2e-browser"})
	if res.Status != 200 || decode[struct {
		Email string `json:"email"`
	}](t, res).Email != email {
		t.Fatalf("code: %d %s", res.Status, res.Body)
	}
	c := sessionCookieOf(t, res)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/v1/console" ||
		c.MaxAge < int((8*time.Hour-time.Minute).Seconds()) || c.MaxAge > int((8*time.Hour).Seconds()) {
		t.Fatalf("cookie: %+v", c)
	}
	if res = enroll(memberPassword); res.Status != 409 || errorOf(t, res).Code != "totp_already_enrolled" {
		t.Fatalf("enrolling again: %d %s", res.Status, res.Body)
	}
}

func TestConsoleLoginRefusals(t *testing.T) {
	t.Parallel()
	_, email, temp := newMember(t, consoledomain.RoleModerator)
	if res := consoleLogin(t, map[string]string{"email": email}); res.Status != 400 {
		t.Fatalf("no password: %d", res.Status)
	}
	noHeader := call(t, server, "POST", "/v1/console/login", map[string]string{"email": email, "password": temp})
	if noHeader.Status != 400 {
		t.Fatalf("without X-Requested-With: %d %s", noHeader.Status, noHeader.Body)
	}
	for _, try := range [][2]string{{newMemberEmail(), temp}, {email, temp + "x"}} {
		if res := consoleLogin(t, map[string]string{"email": try[0], "password": try[1]}); res.Status != 401 || errorOf(t, res).Code != "invalid_credentials" {
			t.Fatalf("%v: %d %s", try, res.Status, res.Body)
		}
	}
	for _, target := range []string{email, newMemberEmail()} {
		for range 5 {
			consoleLogin(t, map[string]string{"email": target, "password": "wrong password"})
		}
		res := consoleLogin(t, map[string]string{"email": target, "password": temp})
		e := errorOf(t, res)
		if res.Status != 429 || e.Code != "login_locked" || e.Details["retry_after"] == nil {
			t.Fatalf("%s: %d %s", target, res.Status, res.Body)
		}
	}
}

func TestConsoleWrongCodesLock(t *testing.T) {
	t.Parallel()
	s := signInMember(t, consoledomain.RoleModerator)
	good := consoledomain.Code(s.Secret, time.Now().Add(30*time.Second))
	bad := good[:5] + string(rune('0'+(good[5]-'0'+1)%10))
	for range 5 {
		res := consoleLogin(t, map[string]string{"email": s.Email, "password": memberPassword, "code": bad})
		if res.Status != 401 || errorOf(t, res).Code != "invalid_totp_code" {
			t.Fatalf("wrong code: %d %s", res.Status, res.Body)
		}
	}
	if res := consoleLogin(t, map[string]string{"email": s.Email, "password": memberPassword, "code": good}); res.Status != 429 {
		t.Fatalf("after five wrong codes: %d %s", res.Status, res.Body)
	}
}

// The console may send the email as typed; the server trims and lower-cases
// it, so the spec must not refuse it first.
func TestConsoleLoginNormalisesTheEmail(t *testing.T) {
	t.Parallel()
	_, email, temp := newMember(t, consoledomain.RoleModerator)
	res := consoleLogin(t, map[string]string{"email": "  " + strings.ToUpper(email) + " ", "password": temp})
	if errorOf(t, res).Code != "password_change_required" {
		t.Fatalf("a padded, upper-case email: %d %s", res.Status, res.Body)
	}
}
