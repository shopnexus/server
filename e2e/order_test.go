package e2e

import (
	"net/http"
	"testing"
	"time"
)

type orderDeal struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Order  *struct {
		ID           string `json:"id"`
		Code         string `json:"code"`
		Status       string `json:"status"`
		CancelReason string `json:"cancel_reason"`
		Recipient    struct {
			Name   string `json:"name"`
			Phone  string `json:"phone"`
			Street string `json:"street"`
		} `json:"recipient"`
	} `json:"order"`
}

type orderList struct {
	Items []struct {
		DealID string `json:"deal_id"`
	} `json:"items"`
}

// codBody asks for COD to address.
func codBody(address string) map[string]any { return map[string]any{"address_id": address} }

var waybillBody = map[string]any{"carrier": "spx", "tracking_code": "SPXVN0123", "fee": 22_000}

// requestedOrder opens buyer's deal on listing, agrees the listed price and
// asks for COD to buyer's default address.
func requestedOrder(t *testing.T, buyer, listing string) orderDeal {
	t.Helper()
	d := openDeal(t, buyer, listing, http.StatusCreated)
	moved(t, buyer, d.ID, "accept", map[string]any{"price": 1_500_000})
	res := dealCall(t, buyer, d.ID, "cod", codBody(deliveryAddress(t, buyer)))
	if res.Status != http.StatusOK {
		t.Fatalf("cod: %d %s", res.Status, res.Body)
	}
	return decode[orderDeal](t, res)
}

// orderCall posts a flow command on deal id as token, with a fresh
// Idempotency-Key.
func orderCall(t *testing.T, token, id, action string, body any) Response {
	t.Helper()
	return call(t, server, "POST", "/v1/deals/"+id+"/"+action, body, bearer(token), newKey())
}

func getDeal(t *testing.T, token, id string) orderDeal {
	t.Helper()
	res := call(t, server, "GET", "/v1/deals/"+id, nil, bearer(token))
	if res.Status != http.StatusOK {
		t.Fatalf("get: %d %s", res.Status, res.Body)
	}
	return decode[orderDeal](t, res)
}

func listingStatus(t *testing.T, token, id string) string {
	t.Helper()
	res := call(t, server, "GET", "/v1/listings/"+id, nil, bearer(token))
	if res.Status != http.StatusOK {
		t.Fatalf("listing: %d %s", res.Status, res.Body)
	}
	return decode[listing](t, res).Status
}

func TestOrderShipDeliver(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	one, _ := newAccount(t)
	two, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)
	first := requestedOrder(t, one.AccessToken, l.ID)
	second := requestedOrder(t, two.AccessToken, l.ID)
	if first.Order == nil || first.Order.Status != "requested" || first.Order.Code == "" {
		t.Fatalf("%+v", first.Order)
	}

	res := orderCall(t, seller.AccessToken, first.ID, "ship", waybillBody)
	if res.Status != http.StatusOK || decode[orderDeal](t, res).Order.Status != "shipped" {
		t.Fatalf("ship: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "GET", "/v1/listings/"+l.ID, nil); res.Status != http.StatusOK || decode[listing](t, res).Status != "reserved" {
		t.Fatalf("a stranger reads the listing: %d %s", res.Status, res.Body)
	}
	if res = call(t, server, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(seller.AccessToken)); res.Status != http.StatusConflict {
		t.Fatalf("hide a reserved listing: %d", res.Status)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := getDeal(t, two.AccessToken, second.ID)
		if got.Order.Status == "cancelled" {
			if got.Order.CancelReason != "sent_to_another" || got.Status != "negotiating" {
				t.Fatalf("%+v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the other request stayed %s", got.Order.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := getDeal(t, seller.AccessToken, second.ID); got.Order.Recipient.Phone != "" || got.Order.Recipient.Street != "" {
		t.Fatalf("the seller reads a cancelled request: %+v", got.Order.Recipient)
	}

	res = orderCall(t, seller.AccessToken, first.ID, "delivered", nil)
	if got := decode[orderDeal](t, res); res.Status != http.StatusOK || got.Status != "sold" || got.Order.Status != "delivered" {
		t.Fatalf("delivered: %d %s", res.Status, res.Body)
	}
	if st := listingStatus(t, seller.AccessToken, l.ID); st != "sold" {
		t.Fatalf("listing %s", st)
	}
	res = call(t, server, "GET", "/v1/me/orders?role=selling", nil, bearer(seller.AccessToken))
	if list := decode[orderList](t, res); res.Status != http.StatusOK || len(list.Items) != 2 {
		t.Fatalf("orders: %d %s", res.Status, res.Body)
	}
	res = call(t, server, "GET", "/v1/me/orders?role=buying", nil, bearer(one.AccessToken))
	if list := decode[orderList](t, res); res.Status != http.StatusOK || len(list.Items) != 1 || list.Items[0].DealID != first.ID {
		t.Fatalf("buyer's orders: %d %s", res.Status, res.Body)
	}
}

func TestOrderReturn(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken)
	d := requestedOrder(t, buyer.AccessToken, l.ID)
	if res := orderCall(t, seller.AccessToken, d.ID, "ship", waybillBody); res.Status != http.StatusOK {
		t.Fatalf("ship: %d %s", res.Status, res.Body)
	}
	res := orderCall(t, seller.AccessToken, d.ID, "returned", nil)
	if got := decode[orderDeal](t, res); res.Status != http.StatusOK || got.Status != "negotiating" || got.Order.Status != "returned" {
		t.Fatalf("returned: %d %s", res.Status, res.Body)
	}
	if st := listingStatus(t, seller.AccessToken, l.ID); st != "active" {
		t.Fatalf("listing %s", st)
	}
}

func TestOrderRefusals(t *testing.T) {
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
	d := requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID)

	home := deliveryAddress(t, buyer.AccessToken)
	open := openDeal(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID, http.StatusCreated)
	expect("cod signed out", dealCall(t, "", open.ID, "cod", codBody(home)), 401, "unauthenticated")
	expect("cod a bad id", dealCall(t, buyer.AccessToken, "not-a-uuid", "cod", codBody(home)), 400, "")
	expect("cod a bad address id", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody("not-a-uuid")), 400, "")
	expect("cod as a stranger", dealCall(t, stranger.AccessToken, open.ID, "cod", codBody(home)), 404, "deal_not_found")
	expect("cod not agreed", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(home)), 409, "move_not_allowed")
	moved(t, buyer.AccessToken, open.ID, "accept", map[string]any{"price": 1_500_000})
	expect("cod to someone else's address", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(newAddress(t, stranger.AccessToken).ID)),
		404, "address_not_found")
	gone := newAddress(t, buyer.AccessToken).ID
	if res := call(t, server, "DELETE", "/v1/addresses/"+gone, nil, bearer(buyer.AccessToken)); res.Status != http.StatusNoContent {
		t.Fatalf("delete address: %d %s", res.Status, res.Body)
	}
	expect("cod to a deleted address", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(gone)), 404, "address_not_found")
	late, _ := newAccount(t)
	ld := openDeal(t, late.AccessToken, activeListing(t, seller.AccessToken).ID, http.StatusCreated)
	moved(t, late.AccessToken, ld.ID, "accept", map[string]any{"price": 1_500_000})
	lateHome := deliveryAddress(t, late.AccessToken)
	suspend(t, late.Account.ID)
	expect("cod suspended", dealCall(t, late.AccessToken, ld.ID, "cod", codBody(lateHome)), 403, "account_suspended")
	for range 2 {
		requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken).ID)
	}
	expect("a fourth order", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(home)), 409, "cod_limit_reached")

	expect("ship signed out", call(t, server, "POST", "/v1/deals/"+d.ID+"/ship", waybillBody, newKey()), 401, "unauthenticated")
	expect("ship without a key", call(t, server, "POST", "/v1/deals/"+d.ID+"/ship", waybillBody, bearer(seller.AccessToken)), 400, "")
	expect("ship as a stranger", orderCall(t, stranger.AccessToken, d.ID, "ship", waybillBody), 404, "deal_not_found")
	expect("buyer ships", orderCall(t, buyer.AccessToken, d.ID, "ship", waybillBody), 409, "move_not_allowed")
	expect("ship a bad fee", orderCall(t, seller.AccessToken, d.ID, "ship", map[string]any{"carrier": "spx", "tracking_code": "X", "fee": -1}), 422, "invalid_waybill")

	for _, action := range []string{"delivered", "returned"} {
		expect(action+" signed out", call(t, server, "POST", "/v1/deals/"+d.ID+"/"+action, nil, newKey()), 401, "unauthenticated")
		expect(action+" a bad id", orderCall(t, seller.AccessToken, "not-a-uuid", action, nil), 400, "")
		expect(action+" as a stranger", orderCall(t, stranger.AccessToken, d.ID, action, nil), 404, "deal_not_found")
		expect(action+" a requested order", orderCall(t, seller.AccessToken, d.ID, action, nil), 409, "move_not_allowed")
	}

	expect("orders signed out", call(t, server, "GET", "/v1/me/orders?role=buying", nil), 401, "unauthenticated")
	expect("orders a bad role", call(t, server, "GET", "/v1/me/orders?role=bogus", nil, bearer(buyer.AccessToken)), 400, "")
}
