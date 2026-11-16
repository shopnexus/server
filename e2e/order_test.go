package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

// codBody asks for COD to address.
func codBody(address string) map[string]any { return map[string]any{"address_id": address} }

// trackingCode is a GHTK code no other test ships under: the e2e tests
// share one database, and a carrier's report moves every order with its
// code.
func trackingCode() string { return "S1." + strings.ReplaceAll(uuid.NewV7().String(), "-", "") }

// shipmentBody is a manual GHTK shipment under code; activeListing links
// the seller's GHTK and GHN.
func shipmentBody(code string) map[string]any {
	return map[string]any{"mode": "manual", "carrier": "ghtk", "tracking_code": code}
}

var linkedBody = map[string]any{"mode": "linked", "carrier": "ghtk", "service": "standard", "weight_grams": 800, "pickup": "home"}

// shipmentPreview is the ShipmentPreview schema.
type shipmentPreview struct {
	Carrier       string `json:"carrier"`
	TrackingCode  string `json:"tracking_code"`
	RecipientName string `json:"recipient_name"`
	District      string `json:"district"`
	CODAmount     int64  `json:"cod_amount"`
	Status        string `json:"status"`
	Fee           *int64 `json:"fee"`
}

// pendingOrder opens buyer's deal on listing, agrees the listed price and
// asks for COD to buyer's default address.
func pendingOrder(t *testing.T, buyer, listing string) dealView {
	t.Helper()
	d := openDeal(t, buyer, listing, http.StatusCreated)
	moved(t, buyer, d.ID, "accept", map[string]any{"price": 1_500_000})
	return moved(t, buyer, d.ID, "cod", codBody(deliveryAddress(t, buyer)))
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

// shippedOrder is pendingOrder shipped by the seller with token under a
// fresh code.
func shippedOrder(t *testing.T, seller, buyer, listing string) dealView {
	t.Helper()
	return orderMoved(t, seller, pendingOrder(t, buyer, listing).Order.ID, "shipment", shipmentBody(trackingCode()))
}

// editShipment puts a corrected carrier and code on order id as token.
func editShipment(t *testing.T, token, id string, body any) Response {
	t.Helper()
	return call(t, "PUT", "/v1/orders/"+id+"/shipment", body, bearer(token))
}

// setShipmentStatus reports status for code through the fake carrier's
// dev endpoint, as its carrier would.
func setShipmentStatus(t *testing.T, code, status string) Response {
	t.Helper()
	return call(t, "POST", "/v1/dev/shipments/"+code+"/status", map[string]any{"status": status})
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

	code := trackingCode()
	shipped := orderMoved(t, seller.AccessToken, first.Order.ID, "shipment", shipmentBody(" "+code+" "))
	if sh := shipped.Order.Shipment; shipped.Order.Status != "shipped" || sh == nil || sh.Mode != "manual" || sh.Carrier != "ghtk" ||
		sh.TrackingCode != code || sh.Status != "pending_pickup" || sh.Fee == nil || *sh.Fee != 22_000 {
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
	code := trackingCode()
	d := orderMoved(t, seller.AccessToken, pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID).Order.ID,
		"shipment", shipmentBody(code))
	expectStatus(t, setShipmentStatus(t, code, "in_transit"), http.StatusNoContent)
	if got := getDeal(t, buyer.AccessToken, d.ID); got.Order.Status != "shipped" || got.Order.Shipment.Status != "in_transit" {
		t.Fatalf("in transit: %+v", got.Order)
	}
	expectStatus(t, setShipmentStatus(t, code, "delivered"), http.StatusNoContent)
	got := getDeal(t, buyer.AccessToken, d.ID)
	if got.Order.Status != "shipped" || got.Order.Shipment.Status != "delivered" || got.Order.Shipment.DeliveredAt == "" {
		t.Fatalf("delivered: %+v", got.Order)
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
	code := trackingCode()
	d := orderMoved(t, seller.AccessToken, pendingOrder(t, buyer.AccessToken, l.ID).Order.ID, "shipment", shipmentBody(code))
	expectStatus(t, setShipmentStatus(t, code, "failed"), http.StatusNoContent)
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
	moved(t, buyer.AccessToken, d.ID, "accept", map[string]any{"price": 1_500_000})
	again := moved(t, buyer.AccessToken, d.ID, "cod", codBody(deliveryAddress(t, buyer.AccessToken)))
	if again.Order.Status != "pending" || again.Order.ID == d.Order.ID {
		t.Fatalf("buy again: %+v", again.Order)
	}
}

func TestOrderLinked(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	d := orderMoved(t, seller.AccessToken, pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID).Order.ID,
		"shipment", linkedBody)
	if sh := d.Order.Shipment; sh.Mode != "linked" || sh.Carrier != "ghtk" || !strings.HasPrefix(sh.TrackingCode, "GHTK-") ||
		sh.Status != "pending_pickup" {
		t.Fatalf("%+v", sh)
	}
	check(t, "edit a linked shipment", editShipment(t, seller.AccessToken, d.Order.ID,
		map[string]any{"carrier": "ghn", "tracking_code": "GHN1"}), 409, "move_not_allowed")
}

func TestOrderEditShipment(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	d := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	o := d.Order.ID
	ghn := map[string]any{"carrier": "ghn", "tracking_code": "GHN0042"}
	got := expect[dealView](t, editShipment(t, seller.AccessToken, o, ghn), http.StatusOK)
	if sh := got.Order.Shipment; sh.Carrier != "ghn" || sh.TrackingCode != "GHN0042" || sh.Mode != "manual" {
		t.Fatalf("%+v", sh)
	}
	check(t, "edit signed out", editShipment(t, "", o, ghn), 401, "unauthenticated")
	check(t, "edit a bad id", editShipment(t, seller.AccessToken, "not-a-uuid", ghn), 400, "")
	check(t, "edit without a code", editShipment(t, seller.AccessToken, o, map[string]any{"carrier": "ghn"}), 400, "")
	check(t, "edit as a stranger", editShipment(t, stranger.AccessToken, o, ghn), 404, "order_not_found")
	check(t, "edit as the buyer", editShipment(t, buyer.AccessToken, o, ghn), 409, "move_not_allowed")
	check(t, "edit to carrier other", editShipment(t, seller.AccessToken, o, map[string]any{"carrier": "other", "tracking_code": "X1"}),
		422, "invalid_shipment")
	check(t, "edit to an unknown code", editShipment(t, seller.AccessToken, o, map[string]any{"carrier": "ghn", "tracking_code": "NOTFOUND1"}),
		422, "shipment_not_found")
}

func TestShipmentLookup(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	auth := bearer(seller.AccessToken)
	check(t, "lookup before linking", call(t, "GET", "/v1/shipments/lookup?carrier=ghtk&code=S1", nil, auth), 409, "carrier_not_linked")
	linkCarriers(t, seller.AccessToken)
	p := expect[shipmentPreview](t, call(t, "GET", "/v1/shipments/lookup?carrier=ghtk&code=S1", nil, auth), http.StatusOK)
	if p.Carrier != "ghtk" || p.TrackingCode != "S1" || p.CODAmount != 1_500_000 || p.Status != "pending_pickup" || p.Fee == nil ||
		p.RecipientName == "" {
		t.Fatalf("%+v", p)
	}
	check(t, "lookup signed out", call(t, "GET", "/v1/shipments/lookup?carrier=ghtk&code=S1", nil), 401, "unauthenticated")
	check(t, "lookup without a code", call(t, "GET", "/v1/shipments/lookup?carrier=ghtk", nil, auth), 400, "")
	check(t, "lookup carrier other", call(t, "GET", "/v1/shipments/lookup?carrier=other&code=X1", nil, auth), 422, "invalid_shipment")
	check(t, "lookup at a carrier no seller links", call(t, "GET", "/v1/shipments/lookup?carrier=spx&code=SPX1", nil, auth),
		409, "carrier_not_linked")
	check(t, "lookup an unknown code", call(t, "GET", "/v1/shipments/lookup?carrier=ghtk&code=NOTFOUND1", nil, auth),
		404, "shipment_not_found")
}

func TestDevShipmentStatus(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	check(t, "a code no order has", setShipmentStatus(t, trackingCode(), "delivered"), 404, "shipment_not_found")
	code := trackingCode()
	orderMoved(t, seller.AccessToken, pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID).Order.ID,
		"shipment", shipmentBody(code))
	check(t, "an unknown status", setShipmentStatus(t, code, "returned"), 422, "invalid_status")
	check(t, "no status", call(t, "POST", "/v1/dev/shipments/"+code+"/status", map[string]any{}), 400, "")
}

func TestOrderEarlierRefused(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	code := trackingCode()
	first := orderMoved(t, seller.AccessToken, pendingOrder(t, buyer.AccessToken, l.ID).Order.ID, "shipment", shipmentBody(code))
	expectStatus(t, setShipmentStatus(t, code, "failed"), http.StatusNoContent)
	moved(t, buyer.AccessToken, first.ID, "accept", map[string]any{"price": 1_500_000})
	second := moved(t, buyer.AccessToken, first.ID, "cod", codBody(deliveryAddress(t, buyer.AccessToken)))
	orderMoved(t, seller.AccessToken, second.Order.ID, "shipment", shipmentBody(trackingCode()))
	check(t, "confirm the failed order", orderCall(t, buyer.AccessToken, first.Order.ID, "confirm", nil), 409, "move_not_allowed")
	check(t, "ship the failed order", orderCall(t, seller.AccessToken, first.Order.ID, "shipment", shipmentBody(trackingCode())),
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
	check(t, "cod signed out", dealCall(t, "", open.ID, "cod", codBody(home)), 401, "unauthenticated")
	check(t, "cod a bad id", dealCall(t, buyer.AccessToken, "not-a-uuid", "cod", codBody(home)), 400, "")
	check(t, "cod a bad address id", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody("not-a-uuid")), 400, "")
	check(t, "cod as a stranger", dealCall(t, stranger.AccessToken, open.ID, "cod", codBody(home)), 404, "deal_not_found")
	check(t, "cod not agreed", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(home)), 409, "move_not_allowed")
	moved(t, buyer.AccessToken, open.ID, "accept", map[string]any{"price": 1_500_000})
	check(t, "cod to someone else's address", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(newAddress(t, stranger.AccessToken).ID)),
		404, "address_not_found")
	gone := newAddress(t, buyer.AccessToken).ID
	expectStatus(t, call(t, "DELETE", "/v1/addresses/"+gone, nil, bearer(buyer.AccessToken)), http.StatusNoContent)
	check(t, "cod to a deleted address", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(gone)), 404, "address_not_found")
	late, _ := newAccount(t)
	ld := openDeal(t, late.AccessToken, activeListing(t, seller.AccessToken, nil).ID, http.StatusCreated)
	moved(t, late.AccessToken, ld.ID, "accept", map[string]any{"price": 1_500_000})
	lateHome := deliveryAddress(t, late.AccessToken)
	suspend(t, late.Account.ID)
	check(t, "cod suspended", dealCall(t, late.AccessToken, ld.ID, "cod", codBody(lateHome)), 403, "account_suspended")
	for range 2 {
		pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	}
	check(t, "a fourth order", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(home)), 409, "cod_limit_reached")

	o := d.Order.ID
	body := shipmentBody(trackingCode())
	check(t, "ship signed out", orderCall(t, "", o, "shipment", body), 401, "unauthenticated")
	check(t, "ship without a key", call(t, "POST", "/v1/orders/"+o+"/shipment", body, bearer(seller.AccessToken)), 400, "")
	check(t, "ship in no mode", orderCall(t, seller.AccessToken, o, "shipment", map[string]any{"mode": "pigeon"}), 400, "")
	check(t, "ship as a stranger", orderCall(t, stranger.AccessToken, o, "shipment", body), 404, "order_not_found")
	check(t, "ship a deal's id", orderCall(t, seller.AccessToken, d.ID, "shipment", body), 404, "order_not_found")
	check(t, "buyer ships", orderCall(t, buyer.AccessToken, o, "shipment", body), 409, "move_not_allowed")
	check(t, "ship on carrier other", orderCall(t, seller.AccessToken, o, "shipment",
		map[string]any{"mode": "manual", "carrier": "other", "tracking_code": "X1"}), 422, "invalid_shipment")
	check(t, "ship an unknown code", orderCall(t, seller.AccessToken, o, "shipment", shipmentBody("NOTFOUND1")), 422, "shipment_not_found")
	check(t, "ship 0 grams", orderCall(t, seller.AccessToken, o, "shipment",
		map[string]any{"mode": "linked", "carrier": "ghtk", "service": "standard", "weight_grams": 0, "pickup": "home"}), 422, "invalid_shipment")

	check(t, "confirm signed out", orderCall(t, "", o, "confirm", nil), 401, "unauthenticated")
	check(t, "confirm a bad id", orderCall(t, buyer.AccessToken, "not-a-uuid", "confirm", nil), 400, "")
	check(t, "confirm as a stranger", orderCall(t, stranger.AccessToken, o, "confirm", nil), 404, "order_not_found")
	check(t, "confirm a pending order", orderCall(t, buyer.AccessToken, o, "confirm", nil), 409, "move_not_allowed")
}
