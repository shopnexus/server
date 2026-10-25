package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	staffdomain "github.com/shopnexus/server2/internal/staff/domain"
)

func TestStaffFirstSignIn(t *testing.T) {
	t.Parallel()
	_, email, temp := newStaff(t, staffdomain.RoleModerator)
	enroll := func(password string) Response {
		return staffCall(t, staffSession{}, "POST", "/v1/staff/login/totp-enrollment", map[string]string{"email": email, "password": password})
	}
	steps := []struct {
		name string
		res  Response
		want string
	}{
		{"temporary password", staffLogin(t, map[string]string{"email": email, "password": temp}), "password_change_required"},
		{"enrollment first", enroll(temp), "password_change_required"},
		{"weak password", staffLogin(t, map[string]string{"email": email, "password": temp, "new_password": "short"}), "password_too_weak"},
		{"new password", staffLogin(t, map[string]string{"email": email, "password": temp, "new_password": staffPassword}), "totp_enrollment_required"},
		{"temporary again", enroll(temp), "invalid_credentials"},
	}
	for _, s := range steps {
		if got := errorOf(t, s.res).Code; got != s.want {
			t.Fatalf("%s: %d %s, want %s", s.name, s.res.Status, s.res.Body, s.want)
		}
	}
	res := enroll(staffPassword)
	enr := decode[struct {
		Secret string `json:"secret"`
		URI    string `json:"otpauth_uri"`
	}](t, res)
	if res.Status != 200 || enr.Secret == "" || enr.URI == "" {
		t.Fatalf("enrollment: %d %s", res.Status, res.Body)
	}
	if res = staffLogin(t, map[string]string{"email": email, "password": staffPassword}); errorOf(t, res).Code != "totp_required" {
		t.Fatalf("no code: %d %s", res.Status, res.Body)
	}
	code := staffdomain.Code(decodeBase32(t, enr.Secret), time.Now())
	res = staffLogin(t, map[string]string{"email": email, "password": staffPassword, "code": code}, [2]string{"User-Agent", "e2e-browser"})
	if res.Status != 200 || decode[struct {
		Email string `json:"email"`
	}](t, res).Email != email {
		t.Fatalf("code: %d %s", res.Status, res.Body)
	}
	c := sessionCookieOf(t, res)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/v1/staff" ||
		c.MaxAge < int((8*time.Hour-time.Minute).Seconds()) || c.MaxAge > int((8*time.Hour).Seconds()) {
		t.Fatalf("cookie: %+v", c)
	}
	if res = enroll(staffPassword); res.Status != 409 || errorOf(t, res).Code != "totp_already_enrolled" {
		t.Fatalf("enrolling again: %d %s", res.Status, res.Body)
	}
}

func TestStaffLoginRefusals(t *testing.T) {
	t.Parallel()
	_, email, temp := newStaff(t, staffdomain.RoleModerator)
	if res := staffLogin(t, map[string]string{"email": email}); res.Status != 400 {
		t.Fatalf("no password: %d", res.Status)
	}
	noHeader := call(t, server, "POST", "/v1/staff/login", map[string]string{"email": email, "password": temp})
	if noHeader.Status != 400 {
		t.Fatalf("without X-Requested-With: %d %s", noHeader.Status, noHeader.Body)
	}
	for _, try := range [][2]string{{newStaffEmail(), temp}, {email, temp + "x"}} {
		if res := staffLogin(t, map[string]string{"email": try[0], "password": try[1]}); res.Status != 401 || errorOf(t, res).Code != "invalid_credentials" {
			t.Fatalf("%v: %d %s", try, res.Status, res.Body)
		}
	}
	for _, target := range []string{email, newStaffEmail()} {
		for range 5 {
			staffLogin(t, map[string]string{"email": target, "password": "wrong password"})
		}
		res := staffLogin(t, map[string]string{"email": target, "password": temp})
		e := errorOf(t, res)
		if res.Status != 429 || e.Code != "login_locked" || e.Details["retry_after"] == nil {
			t.Fatalf("%s: %d %s", target, res.Status, res.Body)
		}
	}
}

func TestStaffWrongCodesLock(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, staffdomain.RoleModerator)
	good := staffdomain.Code(s.Secret, time.Now().Add(30*time.Second))
	bad := good[:5] + string(rune('0'+(good[5]-'0'+1)%10))
	for range 5 {
		res := staffLogin(t, map[string]string{"email": s.Email, "password": staffPassword, "code": bad})
		if res.Status != 401 || errorOf(t, res).Code != "invalid_totp_code" {
			t.Fatalf("wrong code: %d %s", res.Status, res.Body)
		}
	}
	if res := staffLogin(t, map[string]string{"email": s.Email, "password": staffPassword, "code": good}); res.Status != 429 {
		t.Fatalf("after five wrong codes: %d %s", res.Status, res.Body)
	}
}

// The console may send the email as typed; the server trims and lower-cases
// it, so the spec must not refuse it first.
func TestStaffLoginNormalisesTheEmail(t *testing.T) {
	t.Parallel()
	_, email, temp := newStaff(t, staffdomain.RoleModerator)
	res := staffLogin(t, map[string]string{"email": "  " + strings.ToUpper(email) + " ", "password": temp})
	if errorOf(t, res).Code != "password_change_required" {
		t.Fatalf("a padded, upper-case email: %d %s", res.Status, res.Body)
	}
}
