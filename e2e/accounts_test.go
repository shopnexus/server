package e2e

import (
	"context"
	"testing"
)

type accountView struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

func TestAccountMe(t *testing.T) {
	acc, _ := newAccount(t)
	res := call(t, server, "GET", "/v1/accounts/me", nil, bearer(acc.AccessToken))
	if v := decode[accountView](t, res); res.Status != 200 || v.ID != acc.Account.ID || v.DisplayName != "" || v.Status != "active" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestAccountMeNeedsAToken(t *testing.T) {
	if res := call(t, server, "GET", "/v1/accounts/me", nil); res.Status != 401 || errorOf(t, res).Code != "unauthenticated" {
		t.Fatalf("no token: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, bearer("junk")); res.Status != 401 || errorOf(t, res).Code != "invalid_token" {
		t.Fatalf("junk token: %d %s", res.Status, res.Body)
	}
}

func TestAccountRename(t *testing.T) {
	acc, _ := newAccount(t)
	res := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "  Linh  "}, bearer(acc.AccessToken))
	if res.Status != 200 || decode[accountView](t, res).DisplayName != "Linh" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	for _, c := range []struct {
		body   map[string]string
		status int
		code   string
	}{
		{map[string]string{"display_name": "   "}, 422, "invalid_display_name"},
		{map[string]string{"display_name": "Linh", "status": "active"}, 400, "invalid_request"},
	} {
		if res := call(t, server, "PATCH", "/v1/accounts/me", c.body, bearer(acc.AccessToken)); res.Status != c.status || errorOf(t, res).Code != c.code {
			t.Fatalf("%v: %d %s", c.body, res.Status, res.Body)
		}
	}
	if res := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "Linh"}); res.Status != 401 {
		t.Fatalf("no token: %d %s", res.Status, res.Body)
	}
}

// A suspended person keeps read access until the access token expires;
// writes stop at once.
func TestAccountRenameWhileSuspended(t *testing.T) {
	acc, _ := newAccount(t)
	if _, err := db.Exec(context.Background(),
		`UPDATE accounts SET status = 'suspended', suspend_reason = 'e2e' WHERE id = $1`, acc.Account.ID); err != nil {
		t.Fatal(err)
	}
	if res := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "Linh"}, bearer(acc.AccessToken)); res.Status != 403 {
		t.Fatalf("rename: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, bearer(acc.AccessToken)); res.Status != 200 {
		t.Fatalf("read: %d %s", res.Status, res.Body)
	}
}
