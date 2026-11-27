package e2e

import (
	"context"
	"net/http"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type reviewView struct {
	ID    string   `json:"id"`
	Role  string   `json:"role"`
	Stars int      `json:"stars"`
	Text  string   `json:"text"`
	Tags  []string `json:"tags"`
	Reply *struct {
		Text string `json:"text"`
	} `json:"reply"`
}

// completedOrder is shippedOrder confirmed by the buyer.
func completedOrder(t *testing.T, seller, buyer, listing string) dealView {
	t.Helper()
	d := shippedOrder(t, seller, buyer, listing)
	return orderMoved(t, buyer, d.Order.ID, "confirm", nil)
}

// revealReviews ends the blind window of an order's reviews.
func revealReviews(t *testing.T, order string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE reviews SET visible_at = now() - interval '1 second' WHERE order_id = @order`,
		pgx.StrictNamedArgs{"order": order}); err != nil {
		t.Fatal(err)
	}
}

// UC-TRS-01 E2, E4, E5, E6, E7, E8, E10, E11, E12.
func TestReviews(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	done := completedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	path := "/v1/orders/" + done.Order.ID + "/review"
	put := func(token, p string, body any) Response {
		t.Helper()
		return call(t, "PUT", p, body, bearer(token))
	}

	check(t, "a buyer's tag on a seller", put(buyer.AccessToken, path, map[string]any{"stars": 5, "tags": []string{"on_time"}}), 422, "invalid_review")
	check(t, "review", put(buyer.AccessToken, path, map[string]any{"stars": 5, "text": "Đúng mô tả", "tags": []string{"as_described", "quick_reply"}}), 200, "")
	first := expect[reviewView](t, put(buyer.AccessToken, path, map[string]any{"stars": 4, "text": "Hơi trầy"}), http.StatusOK)
	if first.Role != "seller" || first.Reply != nil || len(first.Tags) != 2 {
		t.Fatalf("buyer's review: %+v", first)
	}
	sellerReviews := "/v1/sellers/" + seller.Account.ID + "/reviews"
	if got := expect[page[reviewView]](t, call(t, "GET", sellerReviews, nil), http.StatusOK); len(got.Items) != 0 {
		t.Fatalf("a hidden review listed: %+v", got)
	}
	reply := func(token, id string, body any) Response {
		t.Helper()
		return call(t, "POST", "/v1/reviews/"+id+"/reply", body, bearer(token))
	}
	check(t, "reply while hidden", reply(seller.AccessToken, first.ID, map[string]any{"text": "Cảm ơn"}), 409, "review_hidden")

	// The seller's review shows both.
	second := expect[reviewView](t, put(seller.AccessToken, path, map[string]any{"stars": 5, "text": "Nhận đúng hẹn", "tags": []string{"on_time"}}), http.StatusOK)
	if second.Role != "buyer" || len(second.Tags) != 1 {
		t.Fatalf("seller's review: %+v", second)
	}
	got := expect[page[reviewView]](t, call(t, "GET", sellerReviews, nil), http.StatusOK)
	if len(got.Items) != 1 || got.Items[0].Text != "Hơi trầy" {
		t.Fatalf("seller reviews: %+v", got)
	}
	asBuyer := expect[page[reviewView]](t, call(t, "GET", "/v1/accounts/"+buyer.Account.ID+"/reviews?role=buyer", nil), http.StatusOK)
	if len(asBuyer.Items) != 1 || asBuyer.Items[0].ID != second.ID {
		t.Fatalf("buyer reviews: %+v", asBuyer)
	}
	check(t, "reviews a bad id", call(t, "GET", "/v1/sellers/x/reviews", nil), 400, "")
	check(t, "reviews an unknown seller", call(t, "GET", "/v1/sellers/"+uuid.NewV7().String()+"/reviews", nil), 404, "account_not_found")
	check(t, "account reviews a bad role", call(t, "GET", "/v1/accounts/"+buyer.Account.ID+"/reviews?role=x", nil), 400, "")
	check(t, "account reviews an unknown one", call(t, "GET", "/v1/accounts/"+uuid.NewV7().String()+"/reviews", nil), 404, "account_not_found")

	check(t, "edit once shown", put(buyer.AccessToken, path, map[string]any{"stars": 1}), 409, "review_locked")
	check(t, "delete once shown", call(t, "DELETE", path, nil, bearer(buyer.AccessToken)), 409, "review_locked")
	check(t, "reply signed out", call(t, "POST", "/v1/reviews/"+first.ID+"/reply", map[string]any{"text": "a"}), 401, "unauthenticated")
	check(t, "reply a bad id", reply(seller.AccessToken, "x", map[string]any{"text": "a"}), 400, "")
	check(t, "reply as a stranger", reply(stranger.AccessToken, first.ID, map[string]any{"text": "a"}), 404, "review_not_found")
	check(t, "reply as the reviewer", reply(buyer.AccessToken, first.ID, map[string]any{"text": "a"}), 404, "review_not_found")
	check(t, "reply empty", reply(seller.AccessToken, first.ID, map[string]any{"text": " "}), 422, "invalid_review")
	replied := expect[reviewView](t, reply(seller.AccessToken, first.ID, map[string]any{"text": "Cảm ơn bạn"}), http.StatusOK)
	if replied.Reply == nil || replied.Reply.Text != "Cảm ơn bạn" {
		t.Fatalf("reply: %+v", replied)
	}
	check(t, "reply twice", reply(seller.AccessToken, first.ID, map[string]any{"text": "Nữa"}), 409, "review_replied")
	check(t, "edit after the reply", put(buyer.AccessToken, path, map[string]any{"stars": 1}), 409, "review_closed")
	check(t, "delete after the reply", call(t, "DELETE", path, nil, bearer(buyer.AccessToken)), 409, "review_closed")

	check(t, "put signed out", put("", path, map[string]any{"stars": 5}), 401, "unauthenticated")
	check(t, "put a bad id", put(buyer.AccessToken, "/v1/orders/x/review", map[string]any{"stars": 5}), 400, "")
	check(t, "put as a stranger", put(stranger.AccessToken, path, map[string]any{"stars": 5}), 404, "order_not_found")
	shipped := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	check(t, "put a shipped order", put(seller.AccessToken, "/v1/orders/"+shipped.Order.ID+"/review", map[string]any{"stars": 5}), 409, "order_not_delivered")
	cancelled := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	moved(t, buyer.AccessToken, cancelled.ID, "cancel", nil)
	check(t, "put a cancelled order", put(buyer.AccessToken, "/v1/orders/"+cancelled.Order.ID+"/review", map[string]any{"stars": 5}), 409, "order_not_delivered")

	check(t, "delete signed out", call(t, "DELETE", path, nil), 401, "unauthenticated")
	check(t, "delete a bad id", call(t, "DELETE", "/v1/orders/x/review", nil, bearer(buyer.AccessToken)), 400, "")
	check(t, "delete without a review", call(t, "DELETE", "/v1/orders/"+cancelled.Order.ID+"/review", nil, bearer(buyer.AccessToken)), 404, "review_not_found")

	fresh := completedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	freshPath := "/v1/orders/" + fresh.Order.ID + "/review"
	check(t, "put nine stars", put(seller.AccessToken, freshPath, map[string]any{"stars": 9}), 422, "invalid_review")
	check(t, "review the second", put(buyer.AccessToken, freshPath, map[string]any{"stars": 3}), 200, "")
	if _, err := db.Exec(context.Background(), `UPDATE reviews SET created_at = now() - interval '8 days' WHERE order_id = @order`,
		pgx.StrictNamedArgs{"order": fresh.Order.ID}); err != nil {
		t.Fatal(err)
	}
	check(t, "edit after 7 days", put(buyer.AccessToken, freshPath, map[string]any{"stars": 1}), 409, "review_locked")
	check(t, "delete after 7 days", call(t, "DELETE", freshPath, nil, bearer(buyer.AccessToken)), 409, "review_locked")

	third := completedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	thirdPath := "/v1/orders/" + third.Order.ID + "/review"
	check(t, "review the third", put(buyer.AccessToken, thirdPath, map[string]any{"stars": 3}), 200, "")
	check(t, "delete", call(t, "DELETE", thirdPath, nil, bearer(buyer.AccessToken)), 204, "")

	suspended, _ := newAccount(t)
	suspend(t, suspended.Account.ID)
	check(t, "put suspended", put(suspended.AccessToken, path, map[string]any{"stars": 5}), 403, "account_suspended")
	check(t, "reply suspended", reply(suspended.AccessToken, first.ID, map[string]any{"text": "a"}), 403, "account_suspended")
}

// UC-TRS-05 A1, E1, E2, E3.
func TestReviewHelpful(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	reader, _ := newAccount(t)
	done := completedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	r := expect[reviewView](t, call(t, "PUT", "/v1/orders/"+done.Order.ID+"/review", map[string]any{"stars": 5}, bearer(buyer.AccessToken)), http.StatusOK)
	path := "/v1/reviews/" + r.ID + "/helpful"
	helpful := func(token string) (count int, marked *bool) {
		t.Helper()
		got := expect[page[struct {
			ID      string `json:"id"`
			Helpful struct {
				Count  int   `json:"count"`
				Marked *bool `json:"marked"`
			} `json:"helpful"`
		}]](t, call(t, "GET", "/v1/sellers/"+seller.Account.ID+"/reviews", nil, bearer(token)), http.StatusOK)
		if len(got.Items) != 1 || got.Items[0].ID != r.ID {
			t.Fatalf("seller reviews: %+v", got)
		}
		return got.Items[0].Helpful.Count, got.Items[0].Helpful.Marked
	}

	check(t, "mark while hidden", call(t, "PUT", path, nil, bearer(reader.AccessToken)), 404, "review_not_found")
	revealReviews(t, done.Order.ID)
	check(t, "mark", call(t, "PUT", path, nil, bearer(reader.AccessToken)), 204, "")
	check(t, "mark again", call(t, "PUT", path, nil, bearer(reader.AccessToken)), 204, "")
	if count, marked := helpful(reader.AccessToken); count != 1 || marked == nil || !*marked {
		t.Fatalf("after the mark: %d %v", count, marked)
	}
	if count, marked := helpful(buyer.AccessToken); count != 1 || marked != nil { // its reviewer may not mark it
		t.Fatalf("the reviewer reads: %d %v", count, marked)
	}
	check(t, "mark own review", call(t, "PUT", path, nil, bearer(buyer.AccessToken)), 409, "own_review")
	check(t, "mark a review about me", call(t, "PUT", path, nil, bearer(seller.AccessToken)), 409, "own_review")
	check(t, "unmark", call(t, "DELETE", path, nil, bearer(reader.AccessToken)), 204, "")
	check(t, "unmark again", call(t, "DELETE", path, nil, bearer(reader.AccessToken)), 204, "")
	if count, marked := helpful(reader.AccessToken); count != 0 || marked == nil || *marked {
		t.Fatalf("after the unmark: %d %v", count, marked)
	}

	check(t, "mark an unknown review", call(t, "PUT", "/v1/reviews/"+uuid.NewV7().String()+"/helpful", nil, bearer(reader.AccessToken)), 404, "review_not_found")
	for _, method := range []string{"PUT", "DELETE"} {
		check(t, method+" signed out", call(t, method, path, nil), 401, "unauthenticated")
		check(t, method+" a bad id", call(t, method, "/v1/reviews/x/helpful", nil, bearer(reader.AccessToken)), 400, "")
	}
	suspend(t, reader.Account.ID)
	for _, method := range []string{"PUT", "DELETE"} {
		check(t, method+" suspended", call(t, method, path, nil, bearer(reader.AccessToken)), 403, "account_suspended")
	}
}
