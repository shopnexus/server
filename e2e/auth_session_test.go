package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/auth/session"
)

type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func refresh(t *testing.T, token string) Response {
	t.Helper()
	return call(t, server, "POST", "/v1/auth/refresh", map[string]string{"refresh_token": token})
}

func TestRefreshRotates(t *testing.T) {
	acc, _ := newAccount(t)
	res := refresh(t, acc.RefreshToken)
	next := decode[tokens](t, res)
	if res.Status != 200 || next.RefreshToken == acc.RefreshToken {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, bearer(next.AccessToken)); res.Status != 200 {
		t.Fatalf("new access token: %d %s", res.Status, res.Body)
	}
}

// TestRefreshReplayRevokesTheSession is the spec's replay attack test.
func TestRefreshReplayRevokesTheSession(t *testing.T) {
	acc, _ := newAccount(t)
	next := decode[tokens](t, refresh(t, acc.RefreshToken))
	for _, token := range []string{acc.RefreshToken, next.RefreshToken} {
		if res := refresh(t, token); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
			t.Fatalf("%d %s", res.Status, res.Body)
		}
	}
}

func TestRefreshRefusals(t *testing.T) {
	if res := refresh(t, "rt_nope"); res.Status != 401 || errorOf(t, res).Code != "invalid_refresh_token" {
		t.Fatalf("unknown: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "POST", "/v1/auth/refresh", map[string]string{}); res.Status != 400 {
		t.Fatalf("empty body: %d %s", res.Status, res.Body)
	}
	acc, _ := newAccount(t)
	db.Exec(context.Background(), `UPDATE accounts SET status = 'suspended', suspend_reason = 'e2e' WHERE id = $1`, acc.Account.ID)
	if res := refresh(t, acc.RefreshToken); res.Status != 403 || errorOf(t, res).Code != "account_suspended" {
		t.Fatalf("suspended: %d %s", res.Status, res.Body)
	}
}

type sessionPage struct {
	Items []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	} `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func sessionOf(t *testing.T, accessToken string) string {
	t.Helper()
	p, err := session.NewVerifier(keys).Verify(accessToken, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return p.SessionID.String()
}

func TestLogoutRevokesRefresh(t *testing.T) {
	acc, _ := newAccount(t)
	if res := call(t, server, "POST", "/v1/auth/logout", nil, bearer(acc.AccessToken)); res.Status != 204 {
		t.Fatalf("logout: %d %s", res.Status, res.Body)
	}
	if res := refresh(t, acc.RefreshToken); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
		t.Fatalf("refresh after logout: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, bearer(acc.AccessToken)); res.Status != 200 {
		t.Fatalf("the access token lives until it expires: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "POST", "/v1/auth/logout", nil); res.Status != 401 {
		t.Fatalf("no token: %d %s", res.Status, res.Body)
	}
}

func TestLogoutAll(t *testing.T) {
	first, phone := newAccount(t)
	second := signInWith(t, phone)
	p, _ := session.NewVerifier(keys).Verify(second.AccessToken, time.Now())
	stale, _, _ := session.NewSigner(keys).Sign(p.AccountID, p.SessionID, time.Now().Add(-20*time.Minute), time.Now())
	if res := call(t, server, "POST", "/v1/auth/logout-all", nil, bearer(stale)); res.Status != 428 || errorOf(t, res).Code != "reauth_required" {
		t.Fatalf("stale sign-in: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "POST", "/v1/auth/logout-all", nil, bearer(second.AccessToken)); res.Status != 204 {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	for _, token := range []string{first.RefreshToken, second.RefreshToken} {
		if res := refresh(t, token); res.Status != 401 {
			t.Fatalf("refresh after logout-all: %d %s", res.Status, res.Body)
		}
	}
	if res := call(t, server, "POST", "/v1/auth/logout-all", nil); res.Status != 401 {
		t.Fatalf("no token: %d %s", res.Status, res.Body)
	}
}

func TestSessionsListAndRemove(t *testing.T) {
	first, phone := newAccount(t)
	second := signInWith(t, phone)
	res := call(t, server, "GET", "/v1/auth/sessions", nil, bearer(second.AccessToken))
	page := decode[sessionPage](t, res)
	if res.Status != 200 || len(page.Items) != 2 || page.Items[0].ID != sessionOf(t, second.AccessToken) || !page.Items[0].Current {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	one := decode[sessionPage](t, call(t, server, "GET", "/v1/auth/sessions?limit=1", nil, bearer(second.AccessToken)))
	rest := decode[sessionPage](t, call(t, server, "GET", "/v1/auth/sessions?limit=1&cursor="+one.NextCursor, nil, bearer(second.AccessToken)))
	if len(rest.Items) != 1 || rest.Items[0].ID != sessionOf(t, first.AccessToken) {
		t.Fatalf("pages %+v, %+v", one, rest)
	}
	if res := call(t, server, "GET", "/v1/auth/sessions?limit=0", nil, bearer(second.AccessToken)); res.Status != 400 {
		t.Fatalf("limit 0: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/auth/sessions", nil); res.Status != 401 {
		t.Fatalf("no token: %d", res.Status)
	}
	if res := call(t, server, "DELETE", "/v1/auth/sessions/"+sessionOf(t, first.AccessToken), nil, bearer(second.AccessToken)); res.Status != 204 {
		t.Fatalf("remove: %d %s", res.Status, res.Body)
	}
	if res := refresh(t, first.RefreshToken); res.Status != 401 {
		t.Fatalf("removed device refreshes: %d", res.Status)
	}
	if res := call(t, server, "DELETE", "/v1/auth/sessions/not-a-uuid", nil, bearer(second.AccessToken)); res.Status != 400 {
		t.Fatalf("bad id: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "DELETE", "/v1/auth/sessions/"+sessionOf(t, second.AccessToken), nil); res.Status != 401 {
		t.Fatalf("no token: %d", res.Status)
	}
}

// TestSessionsOfAnotherAccount is the spec's ownership attack test.
func TestSessionsOfAnotherAccount(t *testing.T) {
	a, _ := newAccount(t)
	b, _ := newAccount(t)
	aSession := sessionOf(t, a.AccessToken)
	for _, s := range decode[sessionPage](t, call(t, server, "GET", "/v1/auth/sessions", nil, bearer(b.AccessToken))).Items {
		if s.ID == aSession {
			t.Fatal("B sees A's session")
		}
	}
	if res := call(t, server, "DELETE", "/v1/auth/sessions/"+aSession, nil, bearer(b.AccessToken)); res.Status != 404 || errorOf(t, res).Code != "session_not_found" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	if res := refresh(t, a.RefreshToken); res.Status != 200 {
		t.Fatalf("A's session after B's attempt: %d %s", res.Status, res.Body)
	}
}
