package e2e

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// An account that saved none reads the defaults in the request's language;
// a saved list reads the same in any language.
func TestQuickReplies(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	type replies struct {
		Items []string `json:"items"`
	}
	english := [2]string{"Accept-Language", "en"}
	vi := expect[replies](t, call(t, "GET", "/v1/me/quick-replies", nil, bearer(me.AccessToken)), http.StatusOK)
	res := call(t, "GET", "/v1/me/quick-replies", nil, bearer(me.AccessToken), english)
	en := expect[replies](t, res, http.StatusOK)
	if len(vi.Items) != 3 || vi.Items[0] != "Còn bạn ơi." || len(en.Items) != 3 || en.Items[0] != "Yes, still available." ||
		res.Header.Get("Vary") != "Accept-Language" {
		t.Fatalf("defaults vi %q en %q, Vary %q", vi.Items, en.Items, res.Header.Get("Vary"))
	}
	want := []string{"Còn bạn ơi", "Tối nay mình rảnh"}
	if got := expect[replies](t, call(t, "PUT", "/v1/me/quick-replies", replies{Items: want}, bearer(me.AccessToken)), http.StatusOK); !slices.Equal(got.Items, want) {
		t.Fatalf("put %q", got.Items)
	}
	if got := expect[replies](t, call(t, "GET", "/v1/me/quick-replies", nil, bearer(me.AccessToken), english), http.StatusOK); !slices.Equal(got.Items, want) {
		t.Fatalf("saved, read in English: %q", got.Items)
	}
}

func TestQuickRepliesRefusals(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	put := func(token string, items []string) Response {
		t.Helper()
		return call(t, "PUT", "/v1/me/quick-replies", map[string]any{"items": items}, bearer(token))
	}
	check(t, "get signed out", call(t, "GET", "/v1/me/quick-replies", nil), 401, "unauthenticated")
	check(t, "put signed out", put("", []string{"Còn"}), 401, "unauthenticated")
	check(t, "put 21 answers", put(me.AccessToken, slices.Repeat([]string{"Còn"}, 21)), 400, "")
	check(t, "put a blank answer", put(me.AccessToken, []string{"Còn", "   "}), 400, "")
	check(t, "put a long answer", put(me.AccessToken, []string{strings.Repeat("a", 201)}), 400, "")
}
