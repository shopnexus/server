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

func TestConsoleFindsAccounts(t *testing.T) {
	t.Parallel()
	mod := signInMember(t, consoledomain.RoleModerator)
	customer, phone := newAccount(t)
	for _, q := range []string{phone, customer.Account.ID} {
		// A phone starts with +, which a raw query string reads as a space.
		res := consoleCall(t, mod, "GET", "/v1/console/accounts?"+url.Values{"q": {q}}.Encode(), nil)
		list := decode[page[consoleAccount]](t, res)
		if res.Status != 200 || len(list.Items) != 1 || list.Items[0].ID != customer.Account.ID {
			t.Fatalf("q=%s: %d %s", q, res.Status, res.Body)
		}
	}
	if res := consoleCall(t, mod, "GET", "/v1/console/accounts?q=0907777777", nil); len(decode[page[consoleAccount]](t, res).Items) != 0 {
		t.Fatalf("an unknown phone: %s", res.Body)
	}
	expectStatus(t, consoleCall(t, mod, "GET", "/v1/console/accounts?limit=5", nil), 200) // every account
	expectStatus(t, consoleCall(t, consoleSession{}, "GET", "/v1/console/accounts", nil), 401)
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
	expectStatus(t, consoleCall(t, mod, "POST", path+"/suspend", map[string]string{"reason": "   "}), 422)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	expectStatus(t, consoleCall(t, mod, "POST", path+"/suspend", map[string]string{"reason": "spam", "until": past}), 422) // an end in the past
	res = consoleCall(t, mod, "POST", path+"/suspend", map[string]string{"reason": "spam"})
	if got := decode[consoleAccount](t, res); res.Status != 200 || got.Status != "suspended" || got.Reason != "spam" {
		t.Fatalf("suspend: %d %s", res.Status, res.Body)
	}
	expectStatus(t, call(t, "PATCH", "/v1/accounts/me", map[string]string{"display_name": "New"}, bearer(customer.AccessToken)), 403) // the suspended customer writing
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
	expectStatus(t, consoleCall(t, mod, "GET", unknown, nil), 404)
	expectStatus(t, consoleCall(t, mod, "POST", unknown+"/suspend", map[string]string{"reason": "spam"}), 404)
	expectStatus(t, consoleCall(t, mod, "POST", unknown+"/unsuspend", nil), 404)
}
