package e2e

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
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
	Deal        dealView        `json:"deal"`
	Counterpart map[string]any  `json:"counterpart"`
	OwnReview   *ownReview      `json:"own_review"`
	Ticket      json.RawMessage `json:"ticket"`
}

// ownReview is the OwnReview schema.
type ownReview struct {
	State       string     `json:"state"`
	ID          *string    `json:"id"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
	ChangeUntil *time.Time `json:"change_until"`
	Locked      bool       `json:"locked"`
	Stars       *int       `json:"stars"`
	Text        *string    `json:"text"`
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

// ordersRow is an OrdersScreenItem; OwnReview stays raw so a test tells an
// absent field from a null one.
type ordersRow struct {
	Order       map[string]any  `json:"order"`
	Counterpart map[string]any  `json:"counterpart"`
	OwnReview   json.RawMessage `json:"own_review"`
}

// review decodes the row's own_review, failing the test when it is absent.
func (r ordersRow) review(t *testing.T) ownReview {
	t.Helper()
	var v ownReview
	if err := json.Unmarshal(r.OwnReview, &v); err != nil || len(r.OwnReview) == 0 {
		t.Fatalf("own_review %q: %v", r.OwnReview, err)
	}
	return v
}

func ordersScreen(t *testing.T, token, query string) page[ordersRow] {
	t.Helper()
	return expect[page[ordersRow]](t, call(t, "GET", "/v1/screens/orders?"+query, nil, bearer(token)), http.StatusOK)
}

func TestOrdersScreen(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	nobody, _ := newAccount(t)
	shipped := orderMoved(t, seller.AccessToken, pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID).Order.ID,
		"shipment", map[string]any{
			"mode": "linked", "carrier": "ghn", "service": "express", "weight_grams": 1200, "pickup": "home",
			"pickup_from": map[string]any{"address_id": deliveryAddress(t, seller.AccessToken)},
		}).Order.ID
	cancelled := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID).Order.ID
	orderMoved(t, buyer.AccessToken, cancelled, "cancel", nil)
	review := "/v1/orders/" + shipped + "/review"
	expectStatus(t, call(t, "PUT", review, map[string]any{"stars": 5, "text": "Đúng mô tả"}, bearer(buyer.AccessToken)), http.StatusOK)

	// Each side's rows hold, field for field, the order, deal ID and listing
	// that side reads on the order view, with the other party.
	for _, side := range []struct {
		token, role, other string
	}{{buyer.AccessToken, "buying", seller.Account.ID}, {seller.AccessToken, "selling", buyer.Account.ID}} {
		rows := ordersScreen(t, side.token, "role="+side.role)
		if len(rows.Items) != 2 || rows.NextCursor != "" || rows.Items[0].Order["id"] != cancelled || rows.Items[1].Order["id"] != shipped {
			t.Fatalf("%s rows: %+v", side.role, rows)
		}
		for i, id := range []string{cancelled, shipped} {
			one := expect[struct {
				Order map[string]any `json:"order"`
				Deal  map[string]any `json:"deal"`
			}](t, call(t, "GET", "/v1/screens/order/"+id, nil, bearer(side.token)), http.StatusOK)
			order := maps.Clone(rows.Items[i].Order)
			dealID, listing := order["deal_id"], order["listing"]
			delete(order, "deal_id")
			delete(order, "listing")
			if !reflect.DeepEqual(order, one.Order) || dealID != one.Deal["id"] || !reflect.DeepEqual(listing, one.Deal["listing"]) {
				t.Fatalf("%s row %d:\n%v\n%v", side.role, i, rows.Items[i].Order, one)
			}
			if c := rows.Items[i].Counterpart; c["id"] != side.other || c["identities"] != nil {
				t.Fatalf("%s counterpart: %+v", side.role, c)
			}
		}
	}

	bought := ordersScreen(t, buyer.AccessToken, "role=buying").Items
	if sh, ok := bought[1].Order["shipment"].(map[string]any); !ok || sh["pickup"] != nil {
		t.Fatalf("buyer's shipment: %+v", bought[1].Order["shipment"])
	}
	if r, ok := bought[0].Order["recipient"].(map[string]any); !ok || r["street"] == nil || r["phone"] == nil {
		t.Fatalf("buyer's cancelled recipient: %+v", r)
	}
	if r := bought[1].review(t); r.State != "active" || *r.Stars != 5 || r.Locked || r.ChangeUntil.Sub(*r.CreatedAt) != 7*24*time.Hour {
		t.Fatalf("reviewed row: %+v", r)
	}
	if r := bought[0].review(t); r.State != "none" || r.ID != nil || r.CreatedAt != nil || r.ChangeUntil != nil || r.Locked {
		t.Fatalf("row without a review: %+v", r)
	}

	sold := ordersScreen(t, seller.AccessToken, "role=selling").Items
	for i := range sold {
		if len(sold[i].OwnReview) != 0 {
			t.Fatalf("seller's row %d carries own_review %s", i, sold[i].OwnReview)
		}
	}
	if sh, ok := sold[1].Order["shipment"].(map[string]any); !ok || sh["pickup"] == nil {
		t.Fatalf("seller's shipment: %+v", sold[1].Order["shipment"])
	}
	if r, ok := sold[0].Order["recipient"].(map[string]any); !ok || r["street"] != nil || r["phone"] != nil || r["name"] == nil {
		t.Fatalf("seller's cancelled recipient: %+v", r)
	}

	first := ordersScreen(t, buyer.AccessToken, "role=buying&limit=1")
	if len(first.Items) != 1 || first.Items[0].Order["id"] != cancelled || first.NextCursor != cancelled {
		t.Fatalf("first page: %+v", first)
	}
	second := ordersScreen(t, buyer.AccessToken, "role=buying&limit=1&cursor="+first.NextCursor)
	if len(second.Items) != 1 || second.Items[0].Order["id"] != shipped || second.NextCursor != "" {
		t.Fatalf("second page: %+v", second)
	}

	expectStatus(t, call(t, "DELETE", review, nil, bearer(buyer.AccessToken)), http.StatusNoContent)
	if _, err := db.Exec(context.Background(), `UPDATE reviews SET created_at = now() - interval '8 days' WHERE order_id = @order`, pgx.StrictNamedArgs{"order": shipped}); err != nil {
		t.Fatal(err)
	}
	if r := ordersScreen(t, buyer.AccessToken, "role=buying").Items[1].review(t); r.State != "deleted" || !r.Locked || r.Stars != nil || r.Text != nil || r.ID == nil {
		t.Fatalf("deleted and locked: %+v", r)
	}

	for _, role := range []string{"buying", "selling"} {
		if empty := ordersScreen(t, nobody.AccessToken, "role="+role); empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
			t.Fatalf("empty %s page: %+v", role, empty)
		}
	}
	check(t, "orders screen signed out", call(t, "GET", "/v1/screens/orders?role=buying", nil), http.StatusUnauthorized, "unauthenticated")
	check(t, "orders screen without a role", call(t, "GET", "/v1/screens/orders", nil, bearer(buyer.AccessToken)), http.StatusBadRequest, "")
	check(t, "orders screen a bad role", call(t, "GET", "/v1/screens/orders?role=bogus", nil, bearer(buyer.AccessToken)), http.StatusBadRequest, "")
	check(t, "orders screen a bad cursor", call(t, "GET", "/v1/screens/orders?role=buying&cursor=bad", nil, bearer(buyer.AccessToken)), http.StatusBadRequest, "")
}
