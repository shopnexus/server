package e2e

import (
	"context"
	"net/http"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type followedAccount struct {
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
	At          string `json:"at"`
}

func TestFollows(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	follow := func(token string, body any) Response {
		t.Helper()
		return call(t, "POST", "/v1/follows", body, bearer(token))
	}
	to := map[string]any{"account_id": seller.Account.ID}

	check(t, "follow", follow(buyer.AccessToken, to), 204, "")
	check(t, "follow again", follow(buyer.AccessToken, to), 204, "")
	check(t, "follow signed out", follow("", to), 401, "unauthenticated")
	check(t, "follow a bad id", follow(buyer.AccessToken, map[string]any{"account_id": "x"}), 400, "")
	check(t, "follow an unknown account", follow(buyer.AccessToken, map[string]any{"account_id": uuid.NewV7().String()}), 404, "account_not_found")
	check(t, "follow oneself", follow(buyer.AccessToken, map[string]any{"account_id": buyer.Account.ID}), 422, "invalid_follow")

	list := expect[page[followedAccount]](t, call(t, "GET", "/v1/follows", nil, bearer(buyer.AccessToken)), http.StatusOK)
	if len(list.Items) != 1 || list.Items[0].AccountID != seller.Account.ID || list.Items[0].At == "" {
		t.Fatalf("list: %+v", list)
	}
	check(t, "list signed out", call(t, "GET", "/v1/follows", nil), 401, "unauthenticated")
	check(t, "list a bad cursor", call(t, "GET", "/v1/follows?cursor=x", nil, bearer(buyer.AccessToken)), 400, "")

	check(t, "unfollow", call(t, "DELETE", "/v1/follows/"+seller.Account.ID, nil, bearer(buyer.AccessToken)), 204, "")
	check(t, "unfollow signed out", call(t, "DELETE", "/v1/follows/"+seller.Account.ID, nil), 401, "unauthenticated")
	check(t, "unfollow a bad id", call(t, "DELETE", "/v1/follows/x", nil, bearer(buyer.AccessToken)), 400, "")
	if list = expect[page[followedAccount]](t, call(t, "GET", "/v1/follows", nil, bearer(buyer.AccessToken)), http.StatusOK); len(list.Items) != 0 {
		t.Fatalf("after unfollow: %+v", list)
	}

	// A block, either way, deletes the follow and refuses a new one.
	check(t, "follow before the block", follow(buyer.AccessToken, to), 204, "")
	expectStatus(t, call(t, "POST", "/v1/blocks", map[string]any{"account_id": buyer.Account.ID}, bearer(seller.AccessToken)), http.StatusNoContent)
	check(t, "follow across a block", follow(buyer.AccessToken, to), 403, "blocked")
	if list = expect[page[followedAccount]](t, call(t, "GET", "/v1/follows", nil, bearer(buyer.AccessToken)), http.StatusOK); len(list.Items) != 0 {
		t.Fatalf("the block left the follow: %+v", list)
	}
}

// A follower hears when the seller first puts a listing on sale.
func TestFollowerHearsOfANewListing(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	follower, _ := newAccount(t)
	expectStatus(t, call(t, "POST", "/v1/follows", map[string]any{"account_id": seller.Account.ID}, bearer(follower.AccessToken)), http.StatusNoContent)
	l := activeListing(t, seller.AccessToken, nil)
	got := notices(t, follower.AccessToken, 1)
	if n := got.Items[0]; n.Type != "followed_listing" || n.Target.Kind != "listing" || n.Target.ID != l.ID || !n.Unread {
		t.Fatalf("%+v", n)
	}
}

func TestFollowRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	full, _ := newAccount(t)
	if _, err := db.Exec(context.Background(), `WITH made AS (
    INSERT INTO accounts (id, display_name, status, write_id, created_at, updated_at)
    SELECT gen_random_uuid(), 'Linh', 'active', gen_random_uuid(), now(), now() FROM generate_series(1, 1000)
    RETURNING id)
INSERT INTO account_follows (id, follower_id, followed_id, created_at)
SELECT gen_random_uuid(), @follower, id, now() FROM made`, pgx.StrictNamedArgs{"follower": full.Account.ID}); err != nil {
		t.Fatal(err)
	}
	to := map[string]any{"account_id": seller.Account.ID}
	check(t, "the 1,001st follow", call(t, "POST", "/v1/follows", to, bearer(full.AccessToken)), 409, "follow_limit_reached")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "a suspended follower", call(t, "POST", "/v1/follows", to, bearer(suspended.AccessToken)), 403, "account_suspended")
}
