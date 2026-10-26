package e2e

import (
	"net/http"
	"testing"
	"uuid"
)

type blockList struct {
	Items []struct {
		AccountID   string `json:"account_id"`
		DisplayName string `json:"display_name"`
	} `json:"items"`
}

func TestBlocks(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)
	block := func(token string, body any) Response {
		t.Helper()
		var headers [][2]string
		if token != "" {
			headers = append(headers, bearer(token))
		}
		return call(t, server, "POST", "/v1/blocks", body, headers...)
	}

	expect("block", block(seller.AccessToken, map[string]any{"account_id": buyer.Account.ID}), 204, "")
	expect("block again", block(seller.AccessToken, map[string]any{"account_id": buyer.Account.ID}), 204, "")
	expect("block signed out", block("", map[string]any{"account_id": buyer.Account.ID}), 401, "unauthenticated")
	expect("block a bad id", block(seller.AccessToken, map[string]any{"account_id": "x"}), 400, "")
	expect("block an unknown account", block(seller.AccessToken, map[string]any{"account_id": uuid.NewV7().String()}), 404, "account_not_found")
	expect("block oneself", block(seller.AccessToken, map[string]any{"account_id": seller.Account.ID}), 422, "invalid_block")

	res := call(t, server, "GET", "/v1/blocks", nil, bearer(seller.AccessToken))
	if list := decode[blockList](t, res); res.Status != http.StatusOK || len(list.Items) != 1 || list.Items[0].AccountID != buyer.Account.ID {
		t.Fatalf("list: %d %s", res.Status, res.Body)
	}
	expect("list signed out", call(t, server, "GET", "/v1/blocks", nil), 401, "unauthenticated")
	expect("list a bad cursor", call(t, server, "GET", "/v1/blocks?cursor=x", nil, bearer(seller.AccessToken)), 400, "")

	expect("a blocked buyer opens a deal", call(t, server, "POST", "/v1/deals", map[string]any{"listing_id": l.ID}, bearer(buyer.AccessToken)), 403, "blocked")

	expect("unblock", call(t, server, "DELETE", "/v1/blocks/"+buyer.Account.ID, nil, bearer(seller.AccessToken)), 204, "")
	expect("unblock signed out", call(t, server, "DELETE", "/v1/blocks/"+buyer.Account.ID, nil), 401, "unauthenticated")
	expect("unblock a bad id", call(t, server, "DELETE", "/v1/blocks/x", nil, bearer(seller.AccessToken)), 400, "")
	openDeal(t, buyer.AccessToken, l.ID, http.StatusCreated)
}
