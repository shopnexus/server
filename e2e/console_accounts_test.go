package e2e

import (
	"net/url"
	"testing"
	"time"
	"uuid"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

type consoleAccount struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"suspend_reason"`
}

type consoleAccountList struct {
	Items []consoleAccount `json:"items"`
}

func TestConsoleFindsAccounts(t *testing.T) {
	t.Parallel()
	mod := signInMember(t, consoledomain.RoleModerator)
	customer, phone := newAccount(t)
	for _, q := range []string{phone, customer.Account.ID} {
		// A phone starts with +, which a raw query string reads as a space.
		res := consoleCall(t, mod, "GET", "/v1/console/accounts?"+url.Values{"q": {q}}.Encode(), nil)
		list := decode[consoleAccountList](t, res)
		if res.Status != 200 || len(list.Items) != 1 || list.Items[0].ID != customer.Account.ID {
			t.Fatalf("q=%s: %d %s", q, res.Status, res.Body)
		}
	}
	if res := consoleCall(t, mod, "GET", "/v1/console/accounts?q=0907777777", nil); len(decode[consoleAccountList](t, res).Items) != 0 {
		t.Fatalf("an unknown phone: %s", res.Body)
	}
	if res := consoleCall(t, mod, "GET", "/v1/console/accounts?limit=5", nil); res.Status != 200 {
		t.Fatalf("every account: %d %s", res.Status, res.Body)
	}
	if res := consoleCall(t, consoleSession{}, "GET", "/v1/console/accounts", nil); res.Status != 401 {
		t.Fatalf("without a cookie: %d", res.Status)
	}
}

func TestConsoleSuspendsAnAccount(t *testing.T) {
	t.Parallel()
	mod := signInMember(t, consoledomain.RoleModerator)
	customer, _ := newAccount(t)
	path := "/v1/console/accounts/" + customer.Account.ID

	res := consoleCall(t, mod, "GET", path, nil)
	detail := decode[struct {
		Identities []struct {
			Kind string `json:"kind"`
		} `json:"identities"`
	}](t, res)
	if res.Status != 200 || len(detail.Identities) != 1 || detail.Identities[0].Kind != "phone" {
		t.Fatalf("detail: %d %s", res.Status, res.Body)
	}
	if got := consoleCall(t, mod, "POST", path+"/suspend", map[string]string{"reason": "   "}); got.Status != 422 {
		t.Fatalf("a blank reason: %d %s", got.Status, got.Body)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if got := consoleCall(t, mod, "POST", path+"/suspend", map[string]string{"reason": "spam", "until": past}); got.Status != 422 {
		t.Fatalf("an end in the past: %d %s", got.Status, got.Body)
	}
	res = consoleCall(t, mod, "POST", path+"/suspend", map[string]string{"reason": "spam"})
	if got := decode[consoleAccount](t, res); res.Status != 200 || got.Status != "suspended" || got.Reason != "spam" {
		t.Fatalf("suspend: %d %s", res.Status, res.Body)
	}
	if got := call(t, server, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "New"}, bearer(customer.AccessToken)); got.Status != 403 {
		t.Fatalf("the suspended customer writing: %d %s", got.Status, got.Body)
	}
	res = consoleCall(t, mod, "GET", path, nil)
	history := decode[struct {
		Actions []struct {
			Action string `json:"action"`
			Actor  struct {
				MemberID string `json:"member_id"`
			} `json:"actor"`
		} `json:"actions"`
	}](t, res)
	if len(history.Actions) != 1 || history.Actions[0].Action != "account.suspend" || history.Actions[0].Actor.MemberID != mod.ID {
		t.Fatalf("history: %s", res.Body)
	}
	if got := decode[consoleAccount](t, consoleCall(t, mod, "POST", path+"/unsuspend", nil)); got.Status != "active" {
		t.Fatalf("unsuspend: %+v", got)
	}

	unknown := "/v1/console/accounts/" + uuid.NewV7().String()
	if got := consoleCall(t, mod, "GET", unknown, nil); got.Status != 404 {
		t.Fatalf("get unknown: %d", got.Status)
	}
	if got := consoleCall(t, mod, "POST", unknown+"/suspend", map[string]string{"reason": "spam"}); got.Status != 404 {
		t.Fatalf("suspend unknown: %d", got.Status)
	}
	if got := consoleCall(t, mod, "POST", unknown+"/unsuspend", nil); got.Status != 404 {
		t.Fatalf("unsuspend unknown: %d", got.Status)
	}
}
