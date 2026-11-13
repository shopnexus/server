package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type profileTasksScreen struct {
	Tasks []struct {
		Key  string `json:"key"`
		Done bool   `json:"done"`
	} `json:"tasks"`
	Account struct {
		ID  string `json:"id"`
		Bio string `json:"bio"`
	} `json:"account"`
	Address *addressView `json:"address"`
}

// profileTasks reads the view as the account with token.
func profileTasks(t *testing.T, token string) profileTasksScreen {
	t.Helper()
	return expect[profileTasksScreen](t, call(t, "GET", "/v1/screens/profile-tasks", nil, bearer(token)), http.StatusOK)
}

// steps writes the view's steps in order as "key=done", for one comparison.
func (s profileTasksScreen) steps() string {
	out := make([]string, 0, len(s.Tasks))
	for _, task := range s.Tasks {
		out = append(out, fmt.Sprintf("%s=%t", task.Key, task.Done))
	}
	return strings.Join(out, " ")
}

func TestProfileTasksNewPhoneAccount(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	v := profileTasks(t, me.AccessToken)
	if got, want := v.steps(), "address=false bio=false phone=true avatar=false"; got != want {
		t.Fatalf("steps %q, want %q", got, want)
	}
	if v.Address != nil || v.Account.ID != me.Account.ID {
		t.Fatalf("%+v", v)
	}
}

func TestProfileTasksAllDone(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	expectStatus(t, call(t, "PATCH", "/v1/accounts/me", map[string]any{"bio": "Đồ cũ còn tốt"}, bearer(me.AccessToken)), http.StatusOK)
	setAvatar(t, me.AccessToken)
	added := newAddress(t, me.AccessToken)
	v := profileTasks(t, me.AccessToken)
	if got, want := v.steps(), "address=true bio=true phone=true avatar=true"; got != want {
		t.Fatalf("steps %q, want %q", got, want)
	}
	if v.Address == nil || v.Address.ID != added.ID || v.Account.Bio != "Đồ cũ còn tốt" {
		t.Fatalf("%+v", v)
	}

	expectStatus(t, call(t, "PATCH", "/v1/accounts/me", map[string]any{"avatar_upload_id": nil}, bearer(me.AccessToken)), http.StatusOK)
	if got, want := profileTasks(t, me.AccessToken).steps(), "address=true bio=true phone=true avatar=false"; got != want {
		t.Fatalf("after removing the avatar: steps %q, want %q", got, want)
	}
}

func TestProfileTasksEmailAccount(t *testing.T) {
	t.Parallel()
	me, _ := newEmailAccount(t)
	if got, want := profileTasks(t, me.AccessToken).steps(), "address=false bio=false phone=false avatar=false"; got != want {
		t.Fatalf("steps %q, want %q", got, want)
	}
	expectStatus(t, call(t, "POST", "/v1/accounts/me/identities",
		map[string]string{"phone_token": issuer.SMSToken(newPhone())}, newKey(), bearer(me.AccessToken)), http.StatusOK)
	if got, want := profileTasks(t, me.AccessToken).steps(), "address=false bio=false phone=true avatar=false"; got != want {
		t.Fatalf("after adding a phone: steps %q, want %q", got, want)
	}
}

func TestProfileTasksBlankBio(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	expectStatus(t, call(t, "PATCH", "/v1/accounts/me", map[string]any{"bio": "   "}, bearer(me.AccessToken)), http.StatusOK)
	if got, want := profileTasks(t, me.AccessToken).steps(), "address=false bio=false phone=true avatar=false"; got != want {
		t.Fatalf("steps %q, want %q", got, want)
	}
}

func TestProfileTasksDefaultAddress(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	first := newAddress(t, me.AccessToken)
	second := newAddress(t, me.AccessToken)
	expectStatus(t, call(t, "POST", "/v1/addresses/"+second.ID+"/default", nil, bearer(me.AccessToken)), http.StatusNoContent)
	if v := profileTasks(t, me.AccessToken); v.Address == nil || v.Address.ID != second.ID || !v.Address.IsDefault {
		t.Fatalf("address %+v, want the default %s", v.Address, second.ID)
	}

	expectStatus(t, call(t, "DELETE", "/v1/addresses/"+first.ID, nil, bearer(me.AccessToken)), http.StatusNoContent)
	expectStatus(t, call(t, "DELETE", "/v1/addresses/"+second.ID, nil, bearer(me.AccessToken)), http.StatusNoContent)
	v := profileTasks(t, me.AccessToken)
	if v.Address != nil || v.Tasks[0].Key != "address" || v.Tasks[0].Done {
		t.Fatalf("after emptying the book: %+v", v)
	}
}

// A suspended person keeps read access until the access token expires.
func TestProfileTasksSuspended(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	suspend(t, me.Account.ID)
	profileTasks(t, me.AccessToken)
}

func TestProfileTasksRefusals(t *testing.T) {
	t.Parallel()
	check(t, "signed out", call(t, "GET", "/v1/screens/profile-tasks", nil), http.StatusUnauthorized, "unauthenticated")

	gone, _ := newAccount(t)
	expectStatus(t, call(t, "DELETE", "/v1/accounts/me", nil, bearer(gone.AccessToken)), http.StatusNoContent)
	check(t, "deleted account", call(t, "GET", "/v1/screens/profile-tasks", nil, bearer(gone.AccessToken)), http.StatusUnauthorized, "session_revoked")
}
