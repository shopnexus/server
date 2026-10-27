package e2e

import (
	"context"
	"net/http"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type sellerReviews struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
	Items   []struct {
		Stars int    `json:"stars"`
		Text  string `json:"text"`
	} `json:"items"`
}

func TestReviews(t *testing.T) {
	t.Parallel()
	expect := func(name string, res Response, status int, code string) {
		t.Helper()
		if res.Status != status || (code != "" && errorOf(t, res).Code != code) {
			t.Errorf("%s: %d %s", name, res.Status, res.Body)
		}
	}
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	shipped := requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID)
	if res := orderCall(t, seller.AccessToken, shipped.ID, "ship", waybillBody); res.Status != http.StatusOK {
		t.Fatalf("ship: %d %s", res.Status, res.Body)
	}
	path := "/v1/orders/" + shipped.Order.ID + "/review"
	put := func(token, p string, body any) Response {
		t.Helper()
		var headers [][2]string
		if token != "" {
			headers = append(headers, bearer(token))
		}
		return call(t, server, "PUT", p, body, headers...)
	}

	expect("review", put(buyer.AccessToken, path, map[string]any{"stars": 5, "text": "Đúng mô tả"}), 200, "")
	expect("edit", put(buyer.AccessToken, path, map[string]any{"stars": 4, "text": "Hơi trầy"}), 200, "")
	res := call(t, server, "GET", "/v1/sellers/"+seller.Account.ID+"/reviews", nil)
	if got := decode[sellerReviews](t, res); res.Status != http.StatusOK || got.Count != 1 || got.Average != 4 || got.Items[0].Text != "Hơi trầy" {
		t.Fatalf("seller reviews: %d %s", res.Status, res.Body)
	}
	expect("reviews a bad id", call(t, server, "GET", "/v1/sellers/x/reviews", nil), 400, "")
	expect("reviews an unknown seller", call(t, server, "GET", "/v1/sellers/"+uuid.NewV7().String()+"/reviews", nil), 404, "account_not_found")

	expect("put signed out", put("", path, map[string]any{"stars": 5}), 401, "unauthenticated")
	expect("put a bad id", put(buyer.AccessToken, "/v1/orders/x/review", map[string]any{"stars": 5}), 400, "")
	expect("put as a stranger", put(stranger.AccessToken, path, map[string]any{"stars": 5}), 404, "order_not_found")
	expect("put nine stars", put(buyer.AccessToken, path, map[string]any{"stars": 9}), 422, "invalid_review")
	cancelled := requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID)
	if res = call(t, server, "POST", "/v1/deals/"+cancelled.ID+"/cancel", nil, bearer(buyer.AccessToken)); res.Status != http.StatusOK {
		t.Fatalf("cancel: %d %s", res.Status, res.Body)
	}
	expect("put a cancelled order", put(buyer.AccessToken, "/v1/orders/"+cancelled.Order.ID+"/review", map[string]any{"stars": 5}), 409, "order_not_shipped")

	expect("delete signed out", call(t, server, "DELETE", path, nil), 401, "unauthenticated")
	expect("delete a bad id", call(t, server, "DELETE", "/v1/orders/x/review", nil, bearer(buyer.AccessToken)), 400, "")
	expect("delete without a review", call(t, server, "DELETE", "/v1/orders/"+cancelled.Order.ID+"/review", nil, bearer(buyer.AccessToken)), 404, "review_not_found")

	if _, err := db.Exec(context.Background(), `UPDATE reviews SET created_at = now() - interval '8 days' WHERE order_id = @order`,
		pgx.StrictNamedArgs{"order": shipped.Order.ID}); err != nil {
		t.Fatal(err)
	}
	expect("edit after 7 days", put(buyer.AccessToken, path, map[string]any{"stars": 1}), 409, "review_locked")
	expect("delete after 7 days", call(t, server, "DELETE", path, nil, bearer(buyer.AccessToken)), 409, "review_locked")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	expect("put suspended", put(suspended.AccessToken, path, map[string]any{"stars": 5}), 403, "account_suspended")

	fresh := requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID)
	if res = orderCall(t, seller.AccessToken, fresh.ID, "ship", waybillBody); res.Status != http.StatusOK {
		t.Fatalf("ship: %d", res.Status)
	}
	freshPath := "/v1/orders/" + fresh.Order.ID + "/review"
	expect("review the second", put(buyer.AccessToken, freshPath, map[string]any{"stars": 3}), 200, "")
	expect("delete", call(t, server, "DELETE", freshPath, nil, bearer(buyer.AccessToken)), 204, "")
}
