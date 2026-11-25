package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/testenv"
)

// codBody asks for COD at price, the price the buyer saw, to address.
func codBody(price int64, address string) map[string]any {
	return map[string]any{"price": price, "address_id": address}
}

// linkedBody is a shipment on the seller's GHTK; activeListing links the
// seller's GHTK and GHN. The fake makes its code from the order's code, so
// no two orders share one.
var linkedBody = map[string]any{"carrier": "ghtk", "service": "standard", "weight_grams": 800, "pickup": "home"}

// pendingOrder opens buyer's deal on listing and asks for COD at the listed
// price to buyer's default address.
func pendingOrder(t *testing.T, buyer, listing string) dealView {
	t.Helper()
	d := openDeal(t, buyer, listing, http.StatusCreated)
	return moved(t, buyer, d.ID, "cod", codBody(1_500_000, deliveryAddress(t, buyer)))
}

// orderCall posts to /v1/orders/{id}/{action} as token, signed out when it
// is empty, with the fresh Idempotency-Key the order steps need.
func orderCall(t *testing.T, token, id, action string, body any) Response {
	t.Helper()
	return call(t, "POST", "/v1/orders/"+id+"/"+action, body, bearer(token), newKey())
}

// orderMoved posts an order step that must succeed and returns the deal.
func orderMoved(t *testing.T, token, id, action string, body any) dealView {
	t.Helper()
	return expect[dealView](t, orderCall(t, token, id, action, body), http.StatusOK)
}

// shippedOrder is pendingOrder shipped by the seller with token on GHTK.
func shippedOrder(t *testing.T, seller, buyer, listing string) dealView {
	t.Helper()
	return orderMoved(t, seller, pendingOrder(t, buyer, listing).Order.ID, "shipment", linkedBody)
}

// setShipmentStatus reports step for code through the fake carrier's dev
// endpoint, as its carrier would.
func setShipmentStatus(t *testing.T, code, step string) Response {
	t.Helper()
	return call(t, "POST", "/v1/dev/shipments/"+code+"/status", map[string]any{"step": step})
}

func listingStatus(t *testing.T, token, id string) string {
	t.Helper()
	return expect[listing](t, call(t, "GET", "/v1/listings/"+id, nil, bearer(token)), http.StatusOK).Status
}

func TestOrderShipConfirm(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	one, _ := newAccount(t)
	two, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	first := pendingOrder(t, one.AccessToken, l.ID)
	second := pendingOrder(t, two.AccessToken, l.ID)
	if first.Order == nil || first.Order.Status != "pending" || first.Order.Code == "" || first.Order.Shipment != nil {
		t.Fatalf("%+v", first.Order)
	}

	shipped := orderMoved(t, seller.AccessToken, first.Order.ID, "shipment", linkedBody)
	if sh := shipped.Order.Shipment; shipped.Order.Status != "shipped" || sh == nil || sh.Mode != "linked" || sh.Carrier != "ghtk" ||
		!strings.HasPrefix(sh.TrackingCode, "GHTK-") || sh.Status != "pending_pickup" || sh.Fee == nil || *sh.Fee != 22_000 {
		t.Fatalf("ship: %+v", shipped.Order)
	}
	if st := listingStatus(t, "", l.ID); st != "reserved" {
		t.Fatalf("a stranger reads the listing: %s", st)
	}
	expectStatus(t, call(t, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(seller.AccessToken)), http.StatusConflict)
	var got dealView
	testenv.Eventually(t, 10*time.Second, func() error {
		if got = getDeal(t, two.AccessToken, second.ID); got.Order.Status != "cancelled" {
			return fmt.Errorf("the other order stayed %s", got.Order.Status)
		}
		return nil
	})
	if got.Order.CancelReason != "shipped_to_another" || got.Status != "negotiating" {
		t.Fatalf("%+v", got)
	}
	if r := getDeal(t, seller.AccessToken, second.ID).Order.Recipient; r.Phone != "" || r.Street != "" {
		t.Fatalf("the seller reads a cancelled order: %+v", r)
	}

	if got = orderMoved(t, one.AccessToken, first.Order.ID, "confirm", nil); got.Status != "sold" || got.Order.Status != "completed" {
		t.Fatalf("confirm: %+v", got)
	}
	if st := listingStatus(t, seller.AccessToken, l.ID); st != "sold" {
		t.Fatalf("listing %s", st)
	}
	if selling := ordersScreen(t, seller.AccessToken, "role=selling"); len(selling.Items) != 2 {
		t.Fatalf("orders: %+v", selling)
	}
	if buying := ordersScreen(t, one.AccessToken, "role=buying"); len(buying.Items) != 1 || buying.Items[0].Order["deal_id"] != first.ID {
		t.Fatalf("buyer's orders: %+v", buying)
	}
}

// The carrier reports the parcel in transit, then delivered: the order
// stays shipped until the buyer confirms (or 3 days pass).
func TestOrderDelivered(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	d := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	code := d.Order.Shipment.TrackingCode
	expectStatus(t, setShipmentStatus(t, code, "transporting"), http.StatusNoContent)
	if got := getDeal(t, buyer.AccessToken, d.ID); got.Order.Status != "shipped" || got.Order.Shipment.Status != "in_transit" {
		t.Fatalf("in transit: %+v", got.Order)
	}
	// A failed attempt stays in transit: the carrier tries again.
	missed := map[string]any{"step": "delivery_failed", "reason": "Không liên lạc được người nhận"}
	expectStatus(t, call(t, "POST", "/v1/dev/shipments/"+code+"/status", missed), http.StatusNoContent)
	if got := getDeal(t, buyer.AccessToken, d.ID); got.Order.Status != "shipped" || got.Order.Shipment.Status != "in_transit" {
		t.Fatalf("a failed attempt: %+v", got.Order)
	}
	expectStatus(t, setShipmentStatus(t, code, "delivered"), http.StatusNoContent)
	got := getDeal(t, buyer.AccessToken, d.ID)
	if got.Order.Status != "shipped" || got.Order.Shipment.Status != "delivered" || got.Order.Shipment.DeliveredAt == "" {
		t.Fatalf("delivered: %+v", got.Order)
	}
	tr := orderScreen(t, buyer.AccessToken, d.Order.ID).Tracking
	if tr == nil || tr.Step != "delivered" || len(tr.Events) != 3 || tr.Events[1]["reason"] != "Không liên lạc được người nhận" {
		t.Fatalf("tracking: %+v", tr)
	}
	if got = orderMoved(t, buyer.AccessToken, d.Order.ID, "confirm", nil); got.Status != "sold" || got.Order.Status != "completed" {
		t.Fatalf("confirm: %+v", got)
	}
}

// The carrier reports the delivery failed: the order fails at once, the
// listing is back on sale, a late report changes nothing, and the buyer
// may buy again.
func TestOrderFailed(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	d := shippedOrder(t, seller.AccessToken, buyer.AccessToken, l.ID)
	code := d.Order.Shipment.TrackingCode
	expectStatus(t, setShipmentStatus(t, code, "returned"), http.StatusNoContent)
	got := getDeal(t, buyer.AccessToken, d.ID)
	if got.Order.Status != "failed" || got.Status != "negotiating" || got.AgreedPrice != 0 {
		t.Fatalf("failed: %+v", got)
	}
	if st := listingStatus(t, seller.AccessToken, l.ID); st != "active" {
		t.Fatalf("listing %s", st)
	}
	expectStatus(t, setShipmentStatus(t, code, "delivered"), http.StatusNoContent)
	if got = getDeal(t, buyer.AccessToken, d.ID); got.Order.Status != "failed" {
		t.Fatalf("a late report: %+v", got.Order)
	}
	again := moved(t, buyer.AccessToken, d.ID, "cod", codBody(1_500_000, deliveryAddress(t, buyer.AccessToken)))
	if again.Order.Status != "pending" || again.Order.ID == d.Order.ID {
		t.Fatalf("buy again: %+v", again.Order)
	}
}

func TestDevShipmentStatus(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	check(t, "a code no order has", setShipmentStatus(t, "GHTK-"+uuid.NewV7().String(), "delivered"), 404, "shipment_not_found")
	code := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID).Order.Shipment.TrackingCode
	check(t, "a status for a step", setShipmentStatus(t, code, "failed"), 400, "")
	check(t, "no step", call(t, "POST", "/v1/dev/shipments/"+code+"/status", map[string]any{}), 400, "")
}

func TestOrderEarlierRefused(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	first := shippedOrder(t, seller.AccessToken, buyer.AccessToken, l.ID)
	expectStatus(t, setShipmentStatus(t, first.Order.Shipment.TrackingCode, "returned"), http.StatusNoContent)
	second := moved(t, buyer.AccessToken, first.ID, "cod", codBody(1_500_000, deliveryAddress(t, buyer.AccessToken)))
	orderMoved(t, seller.AccessToken, second.Order.ID, "shipment", linkedBody)
	check(t, "confirm the failed order", orderCall(t, buyer.AccessToken, first.Order.ID, "confirm", nil), 409, "move_not_allowed")
	check(t, "ship the failed order", orderCall(t, seller.AccessToken, first.Order.ID, "shipment", linkedBody),
		409, "move_not_allowed")
	if got := getDeal(t, buyer.AccessToken, first.ID); got.Order.ID != second.Order.ID || got.Order.Status != "shipped" {
		t.Fatalf("the second order moved: %+v", got.Order)
	}
}

func TestOrderRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	d := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)

	home := deliveryAddress(t, buyer.AccessToken)
	open := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, http.StatusCreated)
	check(t, "cod signed out", dealCall(t, "", open.ID, "cod", codBody(1_500_000, home)), 401, "unauthenticated")
	check(t, "cod a bad id", dealCall(t, buyer.AccessToken, "not-a-uuid", "cod", codBody(1_500_000, home)), 400, "")
	check(t, "cod a bad address id", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(1_500_000, "not-a-uuid")), 400, "")
	check(t, "cod as a stranger", dealCall(t, stranger.AccessToken, open.ID, "cod", codBody(1_500_000, home)), 404, "deal_not_found")
	check(t, "cod at a price not on offer", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(1_400_000, home)), 409, "price_changed")
	check(t, "cod as the seller", dealCall(t, seller.AccessToken, open.ID, "cod", codBody(1_500_000, deliveryAddress(t, seller.AccessToken))),
		409, "move_not_allowed")
	check(t, "cod without a price", dealCall(t, buyer.AccessToken, open.ID, "cod", map[string]any{"address_id": home}), 400, "")
	check(t, "cod to someone else's address", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(1_500_000, newAddress(t, stranger.AccessToken).ID)),
		404, "address_not_found")
	gone := newAddress(t, buyer.AccessToken).ID
	expectStatus(t, call(t, "DELETE", "/v1/addresses/"+gone, nil, bearer(buyer.AccessToken)), http.StatusNoContent)
	check(t, "cod to a deleted address", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(1_500_000, gone)), 404, "address_not_found")
	late, _ := newAccount(t)
	ld := openDeal(t, late.AccessToken, activeListing(t, seller.AccessToken, nil).ID, http.StatusCreated)
	lateHome := deliveryAddress(t, late.AccessToken)
	suspend(t, late.Account.ID)
	check(t, "cod suspended", dealCall(t, late.AccessToken, ld.ID, "cod", codBody(1_500_000, lateHome)), 403, "account_suspended")
	for range 2 {
		pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	}
	check(t, "a fourth order", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(1_500_000, home)), 409, "cod_limit_reached")

	o := d.Order.ID
	body := linkedBody
	check(t, "ship signed out", orderCall(t, "", o, "shipment", body), 401, "unauthenticated")
	check(t, "ship without a key", call(t, "POST", "/v1/orders/"+o+"/shipment", body, bearer(seller.AccessToken)), 400, "")
	check(t, "ship a manual shipment", orderCall(t, seller.AccessToken, o, "shipment",
		map[string]any{"mode": "manual", "carrier": "ghtk", "tracking_code": "S1"}), 400, "")
	check(t, "ship as a stranger", orderCall(t, stranger.AccessToken, o, "shipment", body), 404, "order_not_found")
	check(t, "ship a deal's id", orderCall(t, seller.AccessToken, d.ID, "shipment", body), 404, "order_not_found")
	check(t, "buyer ships", orderCall(t, buyer.AccessToken, o, "shipment", body), 409, "move_not_allowed")
	check(t, "ship on spx", orderCall(t, seller.AccessToken, o, "shipment",
		map[string]any{"carrier": "spx", "service": "standard", "weight_grams": 800, "pickup": "home"}), 422, "invalid_shipment")
	check(t, "ship 0 grams", orderCall(t, seller.AccessToken, o, "shipment",
		map[string]any{"carrier": "ghtk", "service": "standard", "weight_grams": 0, "pickup": "home"}), 422, "invalid_shipment")

	check(t, "confirm signed out", orderCall(t, "", o, "confirm", nil), 401, "unauthenticated")
	check(t, "confirm a bad id", orderCall(t, buyer.AccessToken, "not-a-uuid", "confirm", nil), 400, "")
	check(t, "confirm as a stranger", orderCall(t, stranger.AccessToken, o, "confirm", nil), 404, "order_not_found")
	check(t, "confirm a pending order", orderCall(t, buyer.AccessToken, o, "confirm", nil), 409, "move_not_allowed")
}

// A repeated Idempotency-Key on the COD request and on the cancel answers
// the deal again; without a key the second call is refused.
func TestCodAndCancelRepeatAKey(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	d := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, http.StatusCreated)
	cod, body := newKey(), codBody(1_500_000, deliveryAddress(t, buyer.AccessToken))
	first := expect[dealView](t, call(t, "POST", "/v1/deals/"+d.ID+"/cod", body, bearer(buyer.AccessToken), cod), http.StatusOK)
	again := expect[dealView](t, call(t, "POST", "/v1/deals/"+d.ID+"/cod", body, bearer(buyer.AccessToken), cod), http.StatusOK)
	if first.Order == nil || again.Order == nil || again.Order.ID != first.Order.ID {
		t.Fatalf("the repeated request: %+v %+v", first.Order, again.Order)
	}
	check(t, "a second request without a key", dealCall(t, buyer.AccessToken, d.ID, "cod", body), http.StatusConflict, "move_not_allowed")
	check(t, "a bad key", call(t, "POST", "/v1/deals/"+d.ID+"/cod", body, bearer(buyer.AccessToken), [2]string{"Idempotency-Key", "x"}), http.StatusBadRequest, "")

	cancel := newKey()
	path := "/v1/orders/" + first.Order.ID + "/cancel"
	expect[dealView](t, call(t, "POST", path, nil, bearer(seller.AccessToken), cancel), http.StatusOK)
	if done := expect[dealView](t, call(t, "POST", path, nil, bearer(seller.AccessToken), cancel), http.StatusOK); done.Order == nil || done.Order.Status != "cancelled" {
		t.Fatalf("the repeated cancel: %+v", done.Order)
	}
	check(t, "a second cancel without a key", call(t, "POST", path, nil, bearer(seller.AccessToken)), http.StatusConflict, "move_not_allowed")
}
