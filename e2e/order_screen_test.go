package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

type selectedOrderScreen struct {
	Order struct {
		ID          string         `json:"id"`
		Status      string         `json:"status"`
		CompletedBy string         `json:"completed_by"`
		Recipient   map[string]any `json:"recipient"`
		Shipment    map[string]any `json:"shipment"`
	} `json:"order"`
	Deal        dealView       `json:"deal"`
	Counterpart map[string]any `json:"counterpart"`
	OwnReview   *struct {
		State       string     `json:"state"`
		ID          *string    `json:"id"`
		CreatedAt   *time.Time `json:"created_at"`
		UpdatedAt   *time.Time `json:"updated_at"`
		ChangeUntil *time.Time `json:"change_until"`
		Locked      bool       `json:"locked"`
		Stars       *int       `json:"stars"`
		Text        *string    `json:"text"`
	} `json:"own_review"`
	Ticket json.RawMessage `json:"ticket"`
}

func orderScreen(t *testing.T, token, order string) selectedOrderScreen {
	t.Helper()
	return expect[selectedOrderScreen](t, call(t, "GET", "/v1/screens/order/"+order, nil, bearer(token)), http.StatusOK)
}

func TestSelectedOrderAndScopedCancel(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	d := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	old := d.Order.ID
	first := orderScreen(t, buyer.AccessToken, old)
	if first.Order.ID != old || first.OwnReview == nil || first.OwnReview.State != "none" || first.OwnReview.ID != nil || first.OwnReview.CreatedAt != nil || first.OwnReview.Locked || string(first.Ticket) != "null" {
		t.Fatalf("new order: %+v", first)
	}
	if first.Counterpart["id"] != seller.Account.ID || first.Counterpart["identities"] != nil {
		t.Fatalf("counterpart: %+v", first.Counterpart)
	}
	orderMoved(t, buyer.AccessToken, old, "cancel", nil)
	moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000})
	current := moved(t, buyer.AccessToken, d.ID, "cod", codBody(deliveryAddress(t, buyer.AccessToken)))
	read := orderScreen(t, seller.AccessToken, old)
	if read.Order.ID != old || read.Order.Status != "cancelled" || read.Deal.Order.ID != current.Order.ID || read.OwnReview != nil || len(read.Ticket) != 0 || read.Order.Recipient["phone"] != nil || read.Order.Recipient["street"] != nil {
		t.Fatalf("historical seller view: %+v", read)
	}
	check(t, "historical cancel", orderCall(t, seller.AccessToken, old, "cancel", nil), 409, "move_not_allowed")
	if getDeal(t, buyer.AccessToken, d.ID).Order.Status != "pending" {
		t.Fatal("old screen cancelled current order")
	}
	orderMoved(t, seller.AccessToken, current.Order.ID, "cancel", nil)
	check(t, "foreign cancel", orderCall(t, stranger.AccessToken, current.Order.ID, "cancel", nil), 404, "order_not_found")
	check(t, "missing cancel", orderCall(t, buyer.AccessToken, uuid.NewV7().String(), "cancel", nil), 404, "order_not_found")
	check(t, "signed out cancel", orderCall(t, "", old, "cancel", nil), 401, "unauthenticated")
	check(t, "bad cancel id", orderCall(t, buyer.AccessToken, "bad", "cancel", nil), 400, "")
	for _, id := range []string{old, uuid.NewV7().String()} {
		check(t, "foreign order screen", call(t, "GET", "/v1/screens/order/"+id, nil, bearer(stranger.AccessToken)), 404, "order_not_found")
	}
	check(t, "signed out screen", call(t, "GET", "/v1/screens/order/"+old, nil), 401, "unauthenticated")
	check(t, "bad screen id", call(t, "GET", "/v1/screens/order/bad", nil, bearer(buyer.AccessToken)), 400, "")
}

func TestOrderScreenOwnReviewAndCompletion(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	d := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	orderMoved(t, buyer.AccessToken, d.Order.ID, "confirm", nil)
	path := "/v1/orders/" + d.Order.ID + "/review"
	expectStatus(t, call(t, "PUT", path, map[string]any{"stars": 5, "text": "Đúng mô tả"}, bearer(buyer.AccessToken)), 200)
	first := orderScreen(t, buyer.AccessToken, d.Order.ID)
	if first.Order.CompletedBy != "buyer" || first.OwnReview.State != "active" || first.OwnReview.Stars == nil || *first.OwnReview.Stars != 5 || first.OwnReview.ChangeUntil.Sub(*first.OwnReview.CreatedAt) != 7*24*time.Hour {
		t.Fatalf("review: %+v", first)
	}
	sellerView := orderScreen(t, seller.AccessToken, d.Order.ID)
	if sellerView.OwnReview != nil || len(sellerView.Ticket) != 0 {
		t.Fatal("seller reads buyer state")
	}
	expectStatus(t, call(t, "DELETE", path, nil, bearer(buyer.AccessToken)), 204)
	deleted := orderScreen(t, buyer.AccessToken, d.Order.ID).OwnReview
	if deleted.State != "deleted" || deleted.Stars != nil || deleted.Text != nil || *deleted.ID != *first.OwnReview.ID || !deleted.CreatedAt.Equal(*first.OwnReview.CreatedAt) {
		t.Fatalf("deleted: %+v", deleted)
	}
	expectStatus(t, call(t, "PUT", path, map[string]any{"stars": 4}, bearer(buyer.AccessToken)), 200)
	reposted := orderScreen(t, buyer.AccessToken, d.Order.ID).OwnReview
	if reposted.State != "active" || !reposted.ChangeUntil.Equal(*first.OwnReview.ChangeUntil) || *reposted.Text != "" {
		t.Fatalf("reposted: %+v", reposted)
	}
	if _, err := db.Exec(context.Background(), `UPDATE reviews SET created_at = now() - interval '8 days' WHERE order_id = @order`, pgx.StrictNamedArgs{"order": d.Order.ID}); err != nil {
		t.Fatal(err)
	}
	if !orderScreen(t, buyer.AccessToken, d.Order.ID).OwnReview.Locked {
		t.Fatal("expired review reads unlocked")
	}
	check(t, "locked review", call(t, "PUT", path, map[string]any{"stars": 1}, bearer(buyer.AccessToken)), 409, "review_locked")
	if _, err := db.Exec(context.Background(), `UPDATE orders SET completed_by = 'system' WHERE id = @order`, pgx.StrictNamedArgs{"order": d.Order.ID}); err != nil {
		t.Fatal(err)
	}
	if orderScreen(t, buyer.AccessToken, d.Order.ID).Order.CompletedBy != "system" {
		t.Fatal("stored completion actor missing")
	}
}
