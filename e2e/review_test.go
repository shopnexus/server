package e2e

import (
	"context"
	"net/http"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type reviewView struct {
	Stars int    `json:"stars"`
	Text  string `json:"text"`
}

func TestReviews(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	shipped := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	path := "/v1/orders/" + shipped.Order.ID + "/review"
	put := func(token, p string, body any) Response {
		t.Helper()
		return call(t, "PUT", p, body, bearer(token))
	}

	check(t, "review", put(buyer.AccessToken, path, map[string]any{"stars": 5, "text": "Đúng mô tả"}), 200, "")
	check(t, "edit", put(buyer.AccessToken, path, map[string]any{"stars": 4, "text": "Hơi trầy"}), 200, "")
	got := expect[page[reviewView]](t, call(t, "GET", "/v1/sellers/"+seller.Account.ID+"/reviews", nil), http.StatusOK)
	if len(got.Items) != 1 || got.Items[0].Text != "Hơi trầy" {
		t.Fatalf("seller reviews: %+v", got)
	}
	check(t, "reviews a bad id", call(t, "GET", "/v1/sellers/x/reviews", nil), 400, "")
	check(t, "reviews an unknown seller", call(t, "GET", "/v1/sellers/"+uuid.NewV7().String()+"/reviews", nil), 404, "account_not_found")

	check(t, "put signed out", put("", path, map[string]any{"stars": 5}), 401, "unauthenticated")
	check(t, "put a bad id", put(buyer.AccessToken, "/v1/orders/x/review", map[string]any{"stars": 5}), 400, "")
	check(t, "put as a stranger", put(stranger.AccessToken, path, map[string]any{"stars": 5}), 404, "order_not_found")
	check(t, "put nine stars", put(buyer.AccessToken, path, map[string]any{"stars": 9}), 422, "invalid_review")
	cancelled := requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	moved(t, buyer.AccessToken, cancelled.ID, "cancel", nil)
	check(t, "put a cancelled order", put(buyer.AccessToken, "/v1/orders/"+cancelled.Order.ID+"/review", map[string]any{"stars": 5}), 409, "order_not_shipped")

	check(t, "delete signed out", call(t, "DELETE", path, nil), 401, "unauthenticated")
	check(t, "delete a bad id", call(t, "DELETE", "/v1/orders/x/review", nil, bearer(buyer.AccessToken)), 400, "")
	check(t, "delete without a review", call(t, "DELETE", "/v1/orders/"+cancelled.Order.ID+"/review", nil, bearer(buyer.AccessToken)), 404, "review_not_found")

	if _, err := db.Exec(context.Background(), `UPDATE reviews SET created_at = now() - interval '8 days' WHERE order_id = @order`,
		pgx.StrictNamedArgs{"order": shipped.Order.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "edit after 7 days", put(buyer.AccessToken, path, map[string]any{"stars": 1}), 409, "review_locked")
	check(t, "delete after 7 days", call(t, "DELETE", path, nil, bearer(buyer.AccessToken)), 409, "review_locked")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "put suspended", put(suspended.AccessToken, path, map[string]any{"stars": 5}), 403, "account_suspended")

	fresh := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	freshPath := "/v1/orders/" + fresh.Order.ID + "/review"
	check(t, "review the second", put(buyer.AccessToken, freshPath, map[string]any{"stars": 3}), 200, "")
	check(t, "delete", call(t, "DELETE", freshPath, nil, bearer(buyer.AccessToken)), 204, "")
}
