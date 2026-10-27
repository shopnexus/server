package e2e

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	authdomain "github.com/shopnexus/server2/internal/auth/domain"
)

type accountView struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatar_url"`
	Status      string `json:"status"`
}

func TestAccountMe(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	res := call(t, server, "GET", "/v1/accounts/me", nil, bearer(acc.AccessToken))
	if v := decode[accountView](t, res); res.Status != 200 || v.ID != acc.Account.ID || v.DisplayName != "" || v.Status != "active" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
}

func TestAccountMeNeedsAToken(t *testing.T) {
	t.Parallel()
	if res := call(t, server, "GET", "/v1/accounts/me", nil); res.Status != 401 || errorOf(t, res).Code != "unauthenticated" {
		t.Fatalf("no token: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, bearer("junk")); res.Status != 401 || errorOf(t, res).Code != "invalid_token" {
		t.Fatalf("junk token: %d %s", res.Status, res.Body)
	}
}

// A valid token for an account that does not exist, which no sign-in issues,
// gets 404 on both methods.
func TestAccountMeUnknownAccount(t *testing.T) {
	t.Parallel()
	s, _ := authdomain.NewSession(uuid.NewV7(), uuid.NewV7(), "", time.Now(), time.Now())
	token, _, _ := authdomain.NewSigner(keys).Sign(s, time.Now())
	if res := call(t, server, "GET", "/v1/accounts/me", nil, bearer(token)); res.Status != 404 || errorOf(t, res).Code != "account_not_found" {
		t.Fatalf("get: %d %s", res.Status, res.Body)
	}
	res := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "Linh"}, bearer(token))
	if res.Status != 404 || errorOf(t, res).Code != "account_not_found" {
		t.Fatalf("patch: %d %s", res.Status, res.Body)
	}
}

// Parallel renames of one account race on its write_id. Each gets 200 or
// 409 try_again, and at least one wins.
func TestAccountUpdateRace(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	statuses := make(chan int, 30)
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			res := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": fmt.Sprint("Linh ", i)}, bearer(acc.AccessToken))
			if res.Status == 409 && errorOf(t, res).Code != "try_again" {
				t.Errorf("409 with %s", res.Body)
			}
			statuses <- res.Status
		})
	}
	wg.Wait()
	close(statuses)
	won := 0
	for s := range statuses {
		if s != 200 && s != 409 {
			t.Fatalf("status %d", s)
		}
		if s == 200 {
			won++
		}
	}
	if won == 0 {
		t.Fatal("no rename won")
	}
}

func TestAccountUpdate(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	res := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "  Linh  ", "bio": " Đồ cũ còn tốt "}, bearer(acc.AccessToken))
	if v := decode[accountView](t, res); res.Status != 200 || v.DisplayName != "Linh" || v.Bio != "Đồ cũ còn tốt" {
		t.Fatalf("status %d: %s", res.Status, res.Body)
	}
	for _, c := range []struct {
		body   map[string]string
		status int
		code   string
	}{
		{map[string]string{"display_name": "   "}, 422, "invalid_account"},
		{map[string]string{"bio": strings.Repeat("ữ", 201)}, 422, "invalid_account"},
		{map[string]string{"display_name": "Linh", "status": "active"}, 400, "invalid_request"},
		{map[string]string{}, 400, "invalid_request"},
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
func TestAccountUpdateWhileSuspended(t *testing.T) {
	t.Parallel()
	acc, _ := newAccount(t)
	suspend(t, acc.Account.ID)
	if res := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "Linh"}, bearer(acc.AccessToken)); res.Status != 403 {
		t.Fatalf("rename: %d %s", res.Status, res.Body)
	}
	if res := call(t, server, "GET", "/v1/accounts/me", nil, bearer(acc.AccessToken)); res.Status != 200 {
		t.Fatalf("read: %d %s", res.Status, res.Body)
	}
}
