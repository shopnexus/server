package e2e

import (
	"net/http"
	"testing"
	"uuid"
)

type blockedAccount struct {
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
}

func TestBlocks(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	block := func(token string, body any) Response {
		t.Helper()
		return call(t, "POST", "/v1/blocks", body, bearer(token))
	}

	check(t, "block", block(seller.AccessToken, map[string]any{"account_id": buyer.Account.ID}), 204, "")
	check(t, "block again", block(seller.AccessToken, map[string]any{"account_id": buyer.Account.ID}), 204, "")
	check(t, "block signed out", block("", map[string]any{"account_id": buyer.Account.ID}), 401, "unauthenticated")
	check(t, "block a bad id", block(seller.AccessToken, map[string]any{"account_id": "x"}), 400, "")
	check(t, "block an unknown account", block(seller.AccessToken, map[string]any{"account_id": uuid.NewV7().String()}), 404, "account_not_found")
	check(t, "block oneself", block(seller.AccessToken, map[string]any{"account_id": seller.Account.ID}), 422, "invalid_block")

	list := expect[page[blockedAccount]](t, call(t, "GET", "/v1/blocks", nil, bearer(seller.AccessToken)), http.StatusOK)
	if len(list.Items) != 1 || list.Items[0].AccountID != buyer.Account.ID {
		t.Fatalf("list: %+v", list)
	}
	check(t, "list signed out", call(t, "GET", "/v1/blocks", nil), 401, "unauthenticated")
	check(t, "list a bad cursor", call(t, "GET", "/v1/blocks?cursor=x", nil, bearer(seller.AccessToken)), 400, "")

	check(t, "a blocked buyer opens a deal", call(t, "POST", "/v1/deals", map[string]any{"listing_id": l.ID}, bearer(buyer.AccessToken)), 403, "blocked")

	check(t, "unblock", call(t, "DELETE", "/v1/blocks/"+buyer.Account.ID, nil, bearer(seller.AccessToken)), 204, "")
	check(t, "unblock signed out", call(t, "DELETE", "/v1/blocks/"+buyer.Account.ID, nil), 401, "unauthenticated")
	check(t, "unblock a bad id", call(t, "DELETE", "/v1/blocks/x", nil, bearer(seller.AccessToken)), 400, "")
	openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
}
