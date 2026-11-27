package e2e

import (
	"testing"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
)

type tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func refresh(t *testing.T, token string) Response {
	t.Helper()
	return call(t, "POST", "/v1/auth/refresh", map[string]string{"refresh_token": token})
}

func TestRefreshRotates(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	res := refresh(t, acc.RefreshToken)
	next := decode[tokens](t, res)
	if res.Status != 200 || next.RefreshToken == acc.RefreshToken {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "GET", "/v1/accounts/me", nil, bearer(next.AccessToken)), 200) // the new access token works
}

// TestRefreshReplayRevokesTheSession is the spec's replay attack test.
func TestRefreshReplayRevokesTheSession(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	next := decode[tokens](t, refresh(t, acc.RefreshToken))
	for _, token := range []string{acc.RefreshToken, next.RefreshToken} {
		if res := refresh(t, token); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
			t.Fatalf("%d %s", res.Status, res.Body)
		}
	}
}

func TestRefreshRefusals(t *testing.T) {
	t.Parallel()
	if res := refresh(t, "rt_nope"); res.Status != 401 || errorOf(t, res).Code != "invalid_refresh_token" {
		t.Fatalf("unknown: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "POST", "/v1/auth/refresh", map[string]string{}), 400)
}

type sessionView struct {
	ID      string `json:"id"`
	Current bool   `json:"current"`
}

func sessionOf(t *testing.T, accessToken string) string {
	t.Helper()
	p, err := accountdomain.NewVerifier(keys).Verify(accessToken, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return p.SessionID.String()
}

func TestDeleteCurrentSessionRevokesRefresh(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/current", nil, bearer(acc.AccessToken)), 204)
	if res := refresh(t, acc.RefreshToken); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
		t.Fatalf("refresh after a sign-out: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "GET", "/v1/accounts/me", nil, bearer(acc.AccessToken)), 200) // the access token lives until it expires
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/current", nil), 401)
}

// Signing out the other devices keeps the caller's: the others' refresh
// tokens answer session_revoked and their push tokens get nothing, and the
// caller's session refreshes and pushes on.
func TestDeleteOtherSessions(t *testing.T) {
	t.Parallel()
	first, phone := newAccount(t)
	second := signInWith(t, phone)
	firstPhone, secondPhone := "first-"+uuid.NewV7().String(), "second-"+uuid.NewV7().String()
	putToken(t, first.AccessToken, firstPhone)
	putToken(t, second.AccessToken, secondPhone)
	p, _ := accountdomain.NewVerifier(keys).Verify(second.AccessToken, time.Now())
	s, _ := accountdomain.NewSession(p.SessionID, p.AccountID, "", time.Now().Add(-20*time.Minute), time.Now())
	stale, _, _ := accountdomain.NewSigner(keys).Sign(s, time.Now())
	if res := call(t, "DELETE", "/v1/auth/sessions/others", nil, bearer(stale)); res.Status != 428 || errorOf(t, res).Code != "reauth_required" {
		t.Fatalf("stale sign-in: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/others", nil, bearer(second.AccessToken)), 204)
	if res := refresh(t, first.RefreshToken); res.Status != 401 || errorOf(t, res).Code != "session_revoked" {
		t.Fatalf("the other device refreshes: %d %s", res.Status, res.Body)
	}
	next := expect[tokens](t, refresh(t, second.RefreshToken), 200)
	devices := expect[page[sessionView]](t, call(t, "GET", "/v1/auth/sessions", nil, bearer(next.AccessToken)), 200)
	if len(devices.Items) != 1 || !devices.Items[0].Current {
		t.Fatalf("devices after the sign-out: %+v", devices)
	}
	activeListing(t, next.AccessToken, nil) // its notice pushes to the account's phones
	phones.wait(t, secondPhone, 1)
	if n := phones.count(firstPhone); n != 0 {
		t.Fatalf("a signed-out device got %d pushes", n)
	}
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/others", nil), 401)
}

func TestSessionsListAndRemove(t *testing.T) {
	t.Parallel()
	first, phone := newAccount(t)
	second := signInWith(t, phone)
	all := expect[page[sessionView]](t, call(t, "GET", "/v1/auth/sessions", nil, bearer(second.AccessToken)), 200)
	if len(all.Items) != 2 || all.Items[0].ID != sessionOf(t, second.AccessToken) || !all.Items[0].Current {
		t.Fatalf("%+v", all)
	}
	one := decode[page[sessionView]](t, call(t, "GET", "/v1/auth/sessions?limit=1", nil, bearer(second.AccessToken)))
	rest := decode[page[sessionView]](t, call(t, "GET", "/v1/auth/sessions?limit=1&cursor="+one.NextCursor, nil, bearer(second.AccessToken)))
	if len(rest.Items) != 1 || rest.Items[0].ID != sessionOf(t, first.AccessToken) {
		t.Fatalf("pages %+v, %+v", one, rest)
	}
	expectStatus(t, call(t, "GET", "/v1/auth/sessions?limit=0", nil, bearer(second.AccessToken)), 400)
	expectStatus(t, call(t, "GET", "/v1/auth/sessions", nil), 401)
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/"+sessionOf(t, first.AccessToken), nil, bearer(second.AccessToken)), 204)
	expectStatus(t, refresh(t, first.RefreshToken), 401) // the removed device can no longer refresh
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/not-a-uuid", nil, bearer(second.AccessToken)), 400)
	expectStatus(t, call(t, "DELETE", "/v1/auth/sessions/"+sessionOf(t, second.AccessToken), nil), 401)
}

// TestSessionsOfAnotherAccount is the spec's ownership attack test.
func TestSessionsOfAnotherAccount(t *testing.T) {
	t.Parallel()
	a, _ := newAccount(t)
	b, _ := newAccount(t)
	aSession := sessionOf(t, a.AccessToken)
	for _, s := range decode[page[sessionView]](t, call(t, "GET", "/v1/auth/sessions", nil, bearer(b.AccessToken))).Items {
		if s.ID == aSession {
			t.Fatal("B sees A's session")
		}
	}
	if res := call(t, "DELETE", "/v1/auth/sessions/"+aSession, nil, bearer(b.AccessToken)); res.Status != 404 || errorOf(t, res).Code != "session_not_found" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	expectStatus(t, refresh(t, a.RefreshToken), 200) // A's session survives B's attempt
}
