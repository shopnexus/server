package e2e

import (
	"strings"
	"testing"

	consoledomain "github.com/shopnexus/server2/internal/console/domain"
)

func TestFeedback(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	body := func(text string) map[string]any { return map[string]any{"body": text} }
	key := newKey()
	text := "Ứng dụng chạy chậm " + newPhone()
	check(t, "send", call(t, "POST", "/v1/feedback", body(text), bearer(me.AccessToken), key), 201, "")
	check(t, "repeat", call(t, "POST", "/v1/feedback", body("khác"), bearer(me.AccessToken), key), 201, "")
	check(t, "blank", call(t, "POST", "/v1/feedback", body("  "), bearer(me.AccessToken), newKey()), 422, "invalid_feedback")
	check(t, "too long", call(t, "POST", "/v1/feedback", body(strings.Repeat("a", 1001)), bearer(me.AccessToken), newKey()), 422, "invalid_feedback")
	check(t, "no key", call(t, "POST", "/v1/feedback", body("x"), bearer(me.AccessToken)), 400, "")
	check(t, "signed out", call(t, "POST", "/v1/feedback", body("x"), newKey()), 401, "unauthenticated")
	for range 9 {
		expectStatus(t, call(t, "POST", "/v1/feedback", body("x"), bearer(me.AccessToken), newKey()), 201)
	}
	check(t, "limit", call(t, "POST", "/v1/feedback", body("x"), bearer(me.AccessToken), newKey()), 429, "feedback_limit_reached")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "suspended", call(t, "POST", "/v1/feedback", body("x"), bearer(suspended.AccessToken), newKey()), 403, "account_suspended")

	mod := signInMember(t, consoledomain.RoleModerator)
	if res := consoleCall(t, mod, "GET", "/v1/console/feedback?limit=100", nil); res.Status != 200 || !strings.Contains(string(res.Body), text) {
		t.Fatalf("console list: %d %s", res.Status, res.Body)
	}
	expectStatus(t, consoleCall(t, consoleSession{}, "GET", "/v1/console/feedback", nil), 401)
}
