package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/staff/member"
)

func TestStaffFirstSignIn(t *testing.T) {
	t.Parallel()
	_, email, temp := newStaff(t, member.RoleModerator)
	step := decode[loginStep](t, staffLogin(t, email, temp))
	if step.Next != "change_password" {
		t.Fatalf("first step %q", step.Next)
	}
	res := call(t, server, "POST", "/v1/staff/login/password", map[string]string{"ticket": step.Ticket, "new_password": "a long e2e staff password"})
	step = decode[loginStep](t, res)
	if step.Next != "enroll_totp" {
		t.Fatalf("second step %q", step.Next)
	}
	res = call(t, server, "POST", "/v1/staff/login/totp-enrollment", map[string]string{"ticket": step.Ticket})
	enr := decode[struct {
		Secret string `json:"secret"`
		URI    string `json:"otpauth_uri"`
	}](t, res)
	if enr.Secret == "" || enr.URI == "" {
		t.Fatalf("%+v", enr)
	}
	s := signInStaff(t, member.RoleAdmin)
	if s.Access == "" || s.Refresh == "" {
		t.Fatalf("%+v", s)
	}
}

func TestStaffRefreshCookieAttributes(t *testing.T) {
	t.Parallel()
	_, email, temp := newStaff(t, member.RoleModerator)
	step := decode[loginStep](t, staffLogin(t, email, temp))
	step = decode[loginStep](t, call(t, server, "POST", "/v1/staff/login/password", map[string]string{"ticket": step.Ticket, "new_password": "a long e2e staff password"}))
	res := call(t, server, "POST", "/v1/staff/login/totp-enrollment", map[string]string{"ticket": step.Ticket})
	secret := decodeBase32(t, decode[struct {
		Secret string `json:"secret"`
	}](t, res).Secret)
	res = call(t, server, "POST", "/v1/staff/login/totp", map[string]string{"ticket": step.Ticket, "code": member.Code(secret, time.Now())}, [2]string{"User-Agent", "e2e-browser"})
	c := refreshCookieOf(t, res)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/v1/staff/session" ||
		c.MaxAge < int((8*time.Hour-time.Minute).Seconds()) || c.MaxAge > int((8*time.Hour).Seconds()) {
		t.Fatalf("%+v", c)
	}
}

func TestStaffLoginRefusals(t *testing.T) {
	t.Parallel()
	_, email, temp := newStaff(t, member.RoleModerator)
	if res := staffLogin(t, "not an email", temp); res.Status != 400 {
		t.Fatalf("malformed email: %d", res.Status)
	}
	for _, try := range [][2]string{{newStaffEmail(), temp}, {email, temp + "x"}} {
		if res := staffLogin(t, try[0], try[1]); res.Status != 401 || errorOf(t, res).Code != "invalid_credentials" {
			t.Fatalf("%v: %d %s", try, res.Status, res.Body)
		}
	}
	for _, target := range []string{email, newStaffEmail()} {
		for range 5 {
			staffLogin(t, target, "wrong password")
		}
		res := staffLogin(t, target, temp)
		e := errorOf(t, res)
		if res.Status != 429 || e.Code != "login_locked" || e.Details["retry_after"] == nil {
			t.Fatalf("%s: %d %s", target, res.Status, res.Body)
		}
	}
}

func TestStaffLoginStepRefusals(t *testing.T) {
	t.Parallel()
	_, email, temp := newStaff(t, member.RoleModerator)
	step := decode[loginStep](t, staffLogin(t, email, temp))
	if res := call(t, server, "POST", "/v1/staff/login/password", map[string]string{"ticket": "garbage", "new_password": "a long e2e staff password"}); res.Status != 401 || errorOf(t, res).Code != "invalid_login_ticket" {
		t.Fatalf("bad ticket: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "POST", "/v1/staff/login/password", map[string]string{"ticket": step.Ticket, "new_password": "short"}); res.Status != 422 || errorOf(t, res).Code != "password_too_weak" {
		t.Fatalf("weak: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "POST", "/v1/staff/login/totp-enrollment", map[string]string{"ticket": step.Ticket}); res.Status != 401 {
		t.Fatalf("enrollment before the password: %d %s", res.Status, res.Body)
	}
}

func TestStaffWrongCodesLock(t *testing.T) {
	t.Parallel()
	s := signInStaff(t, member.RoleModerator)
	step := decode[loginStep](t, staffLogin(t, s.Email, s.Password))
	good := member.Code(s.Secret, time.Now().Add(30*time.Second))
	bad := good[:5] + string(rune('0'+(good[5]-'0'+1)%10))
	for range 5 {
		if res := call(t, server, "POST", "/v1/staff/login/totp", map[string]string{"ticket": step.Ticket, "code": bad}); res.Status != 401 || errorOf(t, res).Code != "invalid_totp_code" {
			t.Fatalf("wrong code: %d %s", res.Status, res.Body)
		}
	}
	if res := call(t, server, "POST", "/v1/staff/login/totp", map[string]string{"ticket": step.Ticket, "code": good}); res.Status != 429 {
		t.Fatalf("after five wrong codes: %d %s", res.Status, res.Body)
	}
}
