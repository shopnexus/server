package e2e

import (
	"net/http"
	"net/url"
	"testing"
	"uuid"
)

type shippingSettings struct {
	Pickup *struct {
		AddressID string `json:"address_id"`
		Phone     string `json:"phone"`
	} `json:"pickup"`
}

type codSetupScreen struct {
	Account         map[string]any   `json:"account"`
	Addresses       []addressView    `json:"addresses"`
	Settings        shippingSettings `json:"settings"`
	CarrierAccounts []carrierAccount `json:"carrier_accounts"`
}

type codCheckoutScreen struct {
	Deal      dealView       `json:"deal"`
	Seller    map[string]any `json:"seller"`
	Address   *addressView   `json:"address"`
	CodOrders struct {
		Used int `json:"used"`
		Max  int `json:"max"`
	} `json:"cod_orders"`
	ShippingEstimate *struct {
		Low  int64 `json:"low"`
		High int64 `json:"high"`
	} `json:"shipping_estimate"`
}

func pickupPreference(address, phone string) map[string]any {
	return map[string]any{"pickup": map[string]any{"address_id": address, "phone": phone}}
}

func TestCodSetupAndSavedPickup(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	auth := bearer(seller.AccessToken)
	setup := func() codSetupScreen {
		t.Helper()
		return expect[codSetupScreen](t, call(t, "GET", "/v1/screens/cod-setup", nil, auth), 200)
	}
	empty := setup()
	if len(empty.Addresses) != 0 || empty.Settings.Pickup != nil || len(empty.CarrierAccounts) != 0 || empty.Account["identities"] == nil {
		t.Fatalf("setup: %+v", empty)
	}
	check(t, "setup signed out", call(t, "GET", "/v1/screens/cod-setup", nil), 401, "unauthenticated")
	path := "/v1/me/shipping-settings"
	address := newAddress(t, seller.AccessToken)
	foreign := newAddress(t, buyer.AccessToken)
	check(t, "settings signed out", call(t, "PUT", path, pickupPreference(address.ID, "0912345678")), 401, "unauthenticated")
	check(t, "missing pickup", call(t, "PUT", path, map[string]any{}, auth), 400, "")
	check(t, "foreign pickup", call(t, "PUT", path, pickupPreference(foreign.ID, "0912345678"), auth), 404, "address_not_found")
	check(t, "missing address", call(t, "PUT", path, pickupPreference(uuid.NewV7().String(), "0912345678"), auth), 404, "address_not_found")
	check(t, "invalid phone", call(t, "PUT", path, pickupPreference(address.ID, "123"), auth), 422, "invalid_shipping_settings")
	stored := expect[shippingSettings](t, call(t, "PUT", path, pickupPreference(address.ID, "0987654321"), auth), 200)
	if stored.Pickup.Phone != "+84987654321" {
		t.Fatalf("phone: %+v", stored)
	}
	body := addressBody()
	body["street"] = "15 Lê Lợi"
	expectStatus(t, call(t, "PUT", "/v1/addresses/"+address.ID, body, auth), 200)
	expectStatus(t, call(t, "PATCH", "/v1/accounts/me", map[string]any{"display_name": "Tên lấy hàng"}, auth), 200)
	d := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	if len(setup().CarrierAccounts) != 2 || setup().Settings.Pickup.AddressID != address.ID || setup().Addresses[0].Street != "15 Lê Lợi" {
		t.Fatal("setup did not compose saved state")
	}
	query := "/v1/shipments/quote?order_id=" + d.Order.ID + "&carrier=ghtk&service=standard&weight_grams=800&saved_pickup=true"
	expectStatus(t, call(t, "GET", query, nil, auth), 200)
	check(t, "exclusive quote selectors", call(t, "GET", query+"&address_id="+url.QueryEscape(address.ID), nil, auth), 422, "invalid_shipment")
	check(t, "buyer cannot quote", call(t, "GET", query, nil, bearer(buyer.AccessToken)), 404, "order_not_found")
	ship := map[string]any{"carrier": "ghtk", "service": "standard", "weight_grams": 800, "pickup": "home", "pickup_from": map[string]any{"saved_pickup": true, "address_id": address.ID}}
	check(t, "exclusive create selectors", orderCall(t, seller.AccessToken, d.Order.ID, "shipment", ship), 422, "invalid_shipment")
	ship["pickup_from"] = map[string]any{"saved_pickup": true}
	orderMoved(t, seller.AccessToken, d.Order.ID, "shipment", ship)
	snapshot, ok := orderScreen(t, seller.AccessToken, d.Order.ID).Order.Shipment["pickup"].(map[string]any)
	if !ok || snapshot["name"] != "Tên lấy hàng" || snapshot["phone"] != "+84987654321" || snapshot["street"] != "15 Lê Lợi" {
		t.Fatalf("pickup snapshot: %+v", snapshot)
	}
	if orderScreen(t, buyer.AccessToken, d.Order.ID).Order.Shipment["pickup"] != nil {
		t.Fatal("buyer reads pickup")
	}
	expectStatus(t, call(t, "DELETE", "/v1/addresses/"+address.ID, nil, auth), 204)
	if setup().Settings.Pickup != nil {
		t.Fatal("deleted address retained contact preference")
	}
	check(t, "missing saved quote", call(t, "GET", query, nil, auth), 409, "pickup_not_configured")
	after, ok := orderScreen(t, seller.AccessToken, d.Order.ID).Order.Shipment["pickup"].(map[string]any)
	if !ok || after["street"] != "15 Lê Lợi" {
		t.Fatal("deletion changed shipment snapshot")
	}
	next := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	check(t, "missing saved shipment", orderCall(t, seller.AccessToken, next.Order.ID, "shipment", ship), 409, "pickup_not_configured")
	cleared := expect[shippingSettings](t, call(t, "PUT", path, map[string]any{"pickup": nil}, auth), http.StatusOK)
	if cleared.Pickup != nil {
		t.Fatal("clear retained pickup")
	}
}

func TestCodCheckoutUsesOnlyBuyerState(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	d := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID, 201)
	path := "/v1/screens/cod-checkout/" + d.ID
	read := func(suffix string) codCheckoutScreen {
		t.Helper()
		return expect[codCheckoutScreen](t, call(t, "GET", path+suffix, nil, bearer(buyer.AccessToken)), 200)
	}
	empty := read("")
	if empty.Address != nil || empty.ShippingEstimate != nil || empty.CodOrders.Used != 0 || empty.CodOrders.Max != 3 || empty.Seller["id"] != seller.Account.ID || empty.Seller["identities"] != nil {
		t.Fatalf("checkout: %+v", empty)
	}
	first := newAddress(t, buyer.AccessToken)
	second := newAddress(t, buyer.AccessToken)
	if read("").Address.ID != first.ID || read("?address_id="+second.ID).Address.ID != second.ID {
		t.Fatal("checkout address selection")
	}
	foreign := newAddress(t, seller.AccessToken)
	for _, id := range []string{foreign.ID, uuid.NewV7().String(), uuid.Nil().String()} {
		check(t, "explicit foreign or absent address", call(t, "GET", path+"?address_id="+id, nil, bearer(buyer.AccessToken)), 404, "address_not_found")
	}
	for _, token := range []string{seller.AccessToken, stranger.AccessToken} {
		check(t, "not buyer", call(t, "GET", path, nil, bearer(token)), 404, "deal_not_found")
	}
	check(t, "no deal", call(t, "GET", "/v1/screens/cod-checkout/"+uuid.NewV7().String(), nil, bearer(buyer.AccessToken)), 404, "deal_not_found")
	check(t, "signed out", call(t, "GET", path, nil), 401, "unauthenticated")
	check(t, "bad deal id", call(t, "GET", "/v1/screens/cod-checkout/bad", nil, bearer(buyer.AccessToken)), 400, "")
	check(t, "bad address id", call(t, "GET", path+"?address_id=bad", nil, bearer(buyer.AccessToken)), 400, "")
	if got := getDeal(t, buyer.AccessToken, d.ID); got.Status != "negotiating" || got.Order != nil {
		t.Fatalf("GET mutated deal: %+v", got)
	}
	moved(t, buyer.AccessToken, d.ID, "cod", codBody(1_500_000, first.ID))
	if read("").CodOrders.Used != 1 || read("").ShippingEstimate != nil {
		t.Fatal("COD cap or estimate")
	}
}

func TestSavedPickupHTTPRetryAfterLostResponse(t *testing.T) {
	t.Parallel()
	for _, deleteAddress := range []bool{false, true} {
		name := "clear_preference"
		if deleteAddress {
			name = "delete_address"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			seller, _ := newAccount(t)
			buyer, _ := newAccount(t)
			auth := bearer(seller.AccessToken)
			address := newAddress(t, seller.AccessToken)
			expectStatus(t, call(t, "PUT", "/v1/me/shipping-settings", pickupPreference(address.ID, "0987654321"), auth), 200)
			d := pendingOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
			path := "/v1/orders/" + d.Order.ID + "/shipment"
			body := map[string]any{"carrier": "ghtk", "service": "standard", "weight_grams": 800, "pickup": "home", "pickup_from": map[string]any{"saved_pickup": true}}
			requestKey := newKey()
			expectStatus(t, call(t, "POST", path, body, auth, requestKey), 200)
			stored := orderScreen(t, seller.AccessToken, d.Order.ID)
			if deleteAddress {
				expectStatus(t, call(t, "DELETE", "/v1/addresses/"+address.ID, nil, auth), 204)
			} else {
				expectStatus(t, call(t, "PUT", "/v1/me/shipping-settings", map[string]any{"pickup": nil}, auth), 200)
			}
			recovered := expect[dealView](t, call(t, "POST", path, body, auth, requestKey), 200)
			if recovered.Order == nil || recovered.Order.ID != d.Order.ID || recovered.Order.Status != "shipped" || recovered.Order.Shipment == nil || recovered.Order.Shipment.TrackingCode != stored.Order.Shipment["tracking_code"] || recovered.Order.Shipment.Pickup == nil || recovered.Order.Shipment.Pickup.Street != address.Street {
				t.Fatalf("lost response retry: %+v", recovered.Order)
			}
			check(t, "quote stays fresh", call(t, "GET", "/v1/shipments/quote?order_id="+d.Order.ID+"&carrier=ghtk&service=standard&weight_grams=800&saved_pickup=true", nil, auth), 409, "pickup_not_configured")
		})
	}
}
