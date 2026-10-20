package e2e

import (
	"context"
	"testing"
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
