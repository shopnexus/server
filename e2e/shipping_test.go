package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// carrierAccount is the CarrierAccount schema.
type carrierAccount struct {
	Carrier  string `json:"carrier"`
	ShopCode string `json:"shop_code"`
}

// linkCarriers links the fake GHTK and GHN to the account with token; a
// second call relinks them.
func linkCarriers(t *testing.T, token string) {
	t.Helper()
	expectStatus(t, call(t, "PUT", "/v1/carrier-accounts/ghtk", map[string]any{"email": "shop@e2e.test", "password": "e2e"}, bearer(token)),
		http.StatusOK)
	expectStatus(t, call(t, "PUT", "/v1/carrier-accounts/ghn", map[string]any{"token": "ghn-e2e", "shop_id": "885412"}, bearer(token)),
		http.StatusOK)
}

func TestLinkListUnlink(t *testing.T) {
	t.Parallel()
	me, _ := newAccount(t)
	auth := bearer(me.AccessToken)
	if l := expect[struct{ Items []carrierAccount }](t, call(t, "GET", "/v1/carrier-accounts", nil, auth), http.StatusOK); len(l.Items) != 0 {
		t.Fatalf("a new account has %+v", l.Items)
	}
	check(t, "a wrong password", call(t, "PUT", "/v1/carrier-accounts/ghtk", map[string]any{"email": "a@b.vn", "password": "wrong"}, auth),
		422, "carrier_login_failed")
	check(t, "a carrier no seller links", call(t, "PUT", "/v1/carrier-accounts/viettel_post",
		map[string]any{"email": "a@b.vn", "password": "x"}, auth), 422, "invalid_carrier")
	check(t, "GHN's body for GHTK", call(t, "PUT", "/v1/carrier-accounts/ghtk", map[string]any{"token": "t", "shop_id": "1"}, auth),
		422, "invalid_carrier")
	check(t, "half a body", call(t, "PUT", "/v1/carrier-accounts/ghtk", map[string]any{"email": "a@b.vn"}, auth), 400, "")
	check(t, "link signed out", call(t, "PUT", "/v1/carrier-accounts/ghn", map[string]any{"token": "t", "shop_id": "1"}), 401, "unauthenticated")
	check(t, "list signed out", call(t, "GET", "/v1/carrier-accounts", nil), 401, "unauthenticated")
	linkCarriers(t, me.AccessToken)
	res := call(t, "GET", "/v1/carrier-accounts", nil, auth)
	l := expect[struct{ Items []carrierAccount }](t, res, http.StatusOK)
	if len(l.Items) != 2 || l.Items[0] != (carrierAccount{"ghn", "885412"}) || l.Items[1].Carrier != "ghtk" ||
		!strings.HasPrefix(l.Items[1].ShopCode, "S") || strings.Contains(string(res.Body), "fake-gh") {
		t.Fatalf("%s", res.Body)
	}

	// An open GHTK shipment keeps GHTK linked; GHN goes.
	buyer, _ := newAccount(t)
	shipped := shippedOrder(t, me.AccessToken, buyer.AccessToken, activeListing(t, me.AccessToken, nil).ID)
	check(t, "unlink with a shipment on the way", call(t, "DELETE", "/v1/carrier-accounts/ghtk", nil, auth), 409, "shipments_open")
	expectStatus(t, call(t, "DELETE", "/v1/carrier-accounts/ghn", nil, auth), http.StatusNoContent)
	check(t, "unlink twice", call(t, "DELETE", "/v1/carrier-accounts/ghn", nil, auth), 404, "carrier_account_not_found")
	check(t, "unlink a carrier no seller links", call(t, "DELETE", "/v1/carrier-accounts/spx", nil, auth), 422, "invalid_carrier")
	check(t, "unlink signed out", call(t, "DELETE", "/v1/carrier-accounts/ghtk", nil), 401, "unauthenticated")
	orderMoved(t, buyer.AccessToken, shipped.Order.ID, "confirm", nil)
	expectStatus(t, call(t, "DELETE", "/v1/carrier-accounts/ghtk", nil, auth), http.StatusNoContent)
}

func TestAnUnlinkedSellerCannotShip(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	o := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID).Order.ID
	expectStatus(t, call(t, "DELETE", "/v1/carrier-accounts/ghtk", nil, bearer(seller.AccessToken)), http.StatusNoContent)
	check(t, "a manual shipment", orderCall(t, seller.AccessToken, o, "shipment", shipmentBody(trackingCode())), 409, "carrier_not_linked")
	check(t, "a linked shipment", orderCall(t, seller.AccessToken, o, "shipment", linkedBody), 409, "carrier_not_linked")
	check(t, "a lookup", call(t, "GET", "/v1/shipments/lookup?carrier=ghtk&code=S1", nil, bearer(seller.AccessToken)), 409, "carrier_not_linked")
	shipped := orderMoved(t, seller.AccessToken, o, "shipment", map[string]any{"mode": "manual", "carrier": "ghn", "tracking_code": "GHN" + trackingCode()})
	check(t, "a correction to GHTK", editShipment(t, seller.AccessToken, shipped.Order.ID,
		map[string]any{"carrier": "ghtk", "tracking_code": "S1"}), 409, "carrier_not_linked")
}

func TestWebhooks(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	shipped := shippedOrder(t, seller.AccessToken, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	code := shipped.Order.Shipment.TrackingCode
	status := func() string {
		t.Helper()
		return getDeal(t, buyer.AccessToken, shipped.ID).Order.Shipment.Status
	}
	expectStatus(t, call(t, "POST", "/v1/carriers/ghtk/webhook/dev", form{"label_id": {code}, "partner_id": {shipped.Order.Code}, "status_id": {"3"}}),
		http.StatusOK)
	if s := status(); s != "in_transit" {
		t.Fatalf("after a form report: %s", s)
	}
	expectStatus(t, call(t, "POST", "/v1/carriers/ghtk/webhook/dev", map[string]any{"label_id": code, "status_id": 5}), http.StatusOK)
	if s := status(); s != "delivered" {
		t.Fatalf("after a JSON report: %s", s)
	}
	check(t, "a wrong secret", call(t, "POST", "/v1/carriers/ghtk/webhook/nope", map[string]any{"label_id": code, "status_id": 9}), 404, "not_found")
	check(t, "a carrier with no webhook", call(t, "POST", "/v1/carriers/spx/webhook/dev", map[string]any{"label_id": code}), 404, "not_found")
	expectStatus(t, call(t, "POST", "/v1/carriers/ghtk/webhook/dev", map[string]any{"label_id": "S1.UNKNOWN", "status_id": 5}), http.StatusOK)
	// A body that does not read is still taken, so GHTK does not send it again.
	for _, r := range []raw{{"application/json", "not json"}, {"text/plain", "label_id=S1"}, {"multipart/form-data; boundary=x", "--x--"}} {
		expectStatus(t, call(t, "POST", "/v1/carriers/ghtk/webhook/dev", r), http.StatusOK)
	}
	check(t, "a wrong secret with a body that does not read", call(t, "POST", "/v1/carriers/ghtk/webhook/nope", raw{"application/json", "{"}),
		404, "not_found")
	expectStatus(t, call(t, "POST", "/v1/carriers/ghn/webhook/dev", map[string]any{"OrderCode": code, "Status": "picking"}), http.StatusOK)
	if s := status(); s != "delivered" {
		t.Fatalf("a GHN notice moved the shipment: %s", s)
	}
}

func TestQuoteAndPickups(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	auth := bearer(seller.AccessToken)
	check(t, "pickups before linking", call(t, "GET", "/v1/carrier-accounts/ghtk/pickups", nil, auth), 409, "carrier_not_linked")
	o := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	pts := expect[struct{ Items []struct{ ID string } }](t, call(t, "GET", "/v1/carrier-accounts/ghtk/pickups", nil, auth), http.StatusOK)
	if len(pts.Items) != 2 || pts.Items[0].ID != "88256" {
		t.Fatalf("%+v", pts)
	}
	check(t, "pickups at a carrier no seller links", call(t, "GET", "/v1/carrier-accounts/spx/pickups", nil, auth), 422, "invalid_carrier")
	check(t, "pickups signed out", call(t, "GET", "/v1/carrier-accounts/ghtk/pickups", nil), 401, "unauthenticated")

	q := url.Values{"order_id": {o.Order.ID}, "carrier": {"ghtk"}, "service": {"standard"}, "weight_grams": {"800"}, "carrier_pickup_id": {"88256"}}
	quote := func(v url.Values, token string) Response {
		t.Helper()
		return call(t, "GET", "/v1/shipments/quote?"+v.Encode(), nil, bearer(token))
	}
	if fee := expect[struct{ Fee int64 }](t, quote(q, seller.AccessToken), http.StatusOK).Fee; fee != 22_000 {
		t.Fatalf("fee %d", fee)
	}
	check(t, "the buyer's quote", quote(q, buyer.AccessToken), 404, "order_not_found")
	check(t, "quote signed out", quote(q, ""), 401, "unauthenticated")
	bad := url.Values{"order_id": {o.Order.ID}, "carrier": {"ghtk"}, "service": {"rocket"}, "weight_grams": {"800"}}
	check(t, "quote a service", quote(bad, seller.AccessToken), 422, "invalid_shipment")
	bad = url.Values{"order_id": {o.Order.ID}, "carrier": {"ghtk"}, "service": {"standard"}}
	check(t, "quote without a weight", quote(bad, seller.AccessToken), 400, "")
	expectStatus(t, call(t, "DELETE", "/v1/carrier-accounts/ghn", nil, auth), http.StatusNoContent)
	q.Set("carrier", "ghn")
	check(t, "quote unlinked", quote(q, seller.AccessToken), 409, "carrier_not_linked")
	linkCarriers(t, seller.AccessToken)

	// A linked shipment from one of the seller's addresses keeps a copy of it.
	from := deliveryAddress(t, seller.AccessToken)
	body := map[string]any{
		"mode": "linked", "carrier": "ghn", "service": "express", "weight_grams": 1200, "pickup": "home",
		"pickup_from": map[string]any{"address_id": from},
	}
	d := orderMoved(t, seller.AccessToken, o.Order.ID, "shipment", body)
	if sh := d.Order.Shipment; sh.Carrier != "ghn" || !strings.HasPrefix(sh.TrackingCode, "GHN-") || sh.Pickup == nil || sh.Pickup.Street == "" {
		t.Fatalf("%+v %+v", sh, sh.Pickup)
	}
}

// A seller links GHN by the code GHN texted: the fake knows no number
// ending 0000 and takes any code but "wrong". Texting is rate tier code.
func TestGHNLinksByATextedCode(t *testing.T) {
	t.Parallel()
	me, phone := newAccount(t)
	auth := bearer(me.AccessToken)
	check(t, "signed out", call(t, "POST", "/v1/carrier-accounts/ghn/otp", map[string]any{"phone": phone}), 401, "unauthenticated")
	check(t, "no phone", call(t, "POST", "/v1/carrier-accounts/ghn/otp", map[string]any{}, auth), 400, "")
	check(t, "a number GHN does not know", call(t, "POST", "/v1/carrier-accounts/ghn/otp", map[string]any{"phone": "+84900000000"}, auth),
		422, "carrier_phone_unknown")
	expectStatus(t, call(t, "POST", "/v1/carrier-accounts/ghn/otp", map[string]any{"phone": phone}, auth), http.StatusNoContent)
	check(t, "a wrong code", call(t, "PUT", "/v1/carrier-accounts/ghn", map[string]any{"phone": phone, "otp": "wrong"}, auth),
		422, "carrier_otp_invalid")
	if a := expect[carrierAccount](t, call(t, "PUT", "/v1/carrier-accounts/ghn", map[string]any{"phone": phone, "otp": "123456"}, auth),
		http.StatusOK); a.Carrier != "ghn" || a.ShopCode == "" {
		t.Fatalf("%+v", a)
	}
	for i := range 20 {
		res := call(t, "POST", "/v1/carrier-accounts/ghn/otp", map[string]any{"phone": phone}, auth)
		if res.Status == http.StatusTooManyRequests {
			check(t, "past the burst", res, 429, "rate_limited")
			return
		}
		if i == 19 {
			t.Fatalf("never limited: %d", res.Status)
		}
	}
}
