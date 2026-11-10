package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/shopnexus/server2/internal/platform/testenv"
)

// codBody asks for COD to address.
func codBody(address string) map[string]any { return map[string]any{"address_id": address} }

var waybillBody = map[string]any{"carrier": "spx", "tracking_code": "SPXVN0123", "fee": 22_000}

// requestedOrder opens buyer's deal on listing, agrees the listed price and
// asks for COD to buyer's default address.
func requestedOrder(t *testing.T, buyer, listing string) dealView {
	t.Helper()
	d := openDeal(t, buyer, listing, http.StatusCreated)
	moved(t, buyer, d.ID, "accept", map[string]any{"price": 1_500_000})
	return moved(t, buyer, d.ID, "cod", codBody(deliveryAddress(t, buyer)))
}

// shippedOrder is requestedOrder shipped by the seller with token.
func shippedOrder(t *testing.T, seller, buyer, listing string) dealView {
	t.Helper()
	return moved(t, seller, requestedOrder(t, buyer, listing).ID, "ship", waybillBody)
}

func listingStatus(t *testing.T, token, id string) string {
	t.Helper()
	return expect[listing](t, call(t, "GET", "/v1/listings/"+id, nil, bearer(token)), http.StatusOK).Status
}

func TestOrderShipDeliver(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	one, _ := newAccount(t)
	two, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	first := requestedOrder(t, one.AccessToken, l.ID)
	second := requestedOrder(t, two.AccessToken, l.ID)
	if first.Order == nil || first.Order.Status != "requested" || first.Order.Code == "" {
		t.Fatalf("%+v", first.Order)
	}

	if shipped := moved(t, seller.AccessToken, first.ID, "ship", waybillBody); shipped.Order.Status != "shipped" {
		t.Fatalf("ship: %+v", shipped.Order)
	}
	if st := listingStatus(t, "", l.ID); st != "reserved" {
		t.Fatalf("a stranger reads the listing: %s", st)
	}
	expectStatus(t, call(t, "POST", "/v1/listings/"+l.ID+"/hide", nil, bearer(seller.AccessToken)), http.StatusConflict)
	var got dealView
	testenv.Eventually(t, 10*time.Second, func() error {
		if got = getDeal(t, two.AccessToken, second.ID); got.Order.Status != "cancelled" {
			return fmt.Errorf("the other request stayed %s", got.Order.Status)
		}
		return nil
	})
	if got.Order.CancelReason != "sent_to_another" || got.Status != "negotiating" {
		t.Fatalf("%+v", got)
	}
	if r := getDeal(t, seller.AccessToken, second.ID).Order.Recipient; r.Phone != "" || r.Street != "" {
		t.Fatalf("the seller reads a cancelled request: %+v", r)
	}

	if got = moved(t, seller.AccessToken, first.ID, "deliver", nil); got.Status != "sold" || got.Order.Status != "delivered" {
		t.Fatalf("deliver: %+v", got)
	}
	if st := listingStatus(t, seller.AccessToken, l.ID); st != "sold" {
		t.Fatalf("listing %s", st)
	}
	type orderRow struct {
		DealID string `json:"deal_id"`
	}
	selling := expect[page[orderRow]](t, call(t, "GET", "/v1/me/orders?role=selling", nil, bearer(seller.AccessToken)), http.StatusOK)
	if len(selling.Items) != 2 {
		t.Fatalf("orders: %+v", selling)
	}
	buying := expect[page[orderRow]](t, call(t, "GET", "/v1/me/orders?role=buying", nil, bearer(one.AccessToken)), http.StatusOK)
	if len(buying.Items) != 1 || buying.Items[0].DealID != first.ID {
		t.Fatalf("buyer's orders: %+v", buying)
	}
}

func TestOrderReturn(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	l := activeListing(t, seller.AccessToken, nil)
	d := shippedOrder(t, seller.AccessToken, buyer.AccessToken, l.ID)
	if got := moved(t, seller.AccessToken, d.ID, "return", nil); got.Status != "negotiating" || got.Order.Status != "returned" {
		t.Fatalf("return: %+v", got)
	}
	if st := listingStatus(t, seller.AccessToken, l.ID); st != "active" {
		t.Fatalf("listing %s", st)
	}
}

func TestOrderRefusals(t *testing.T) {
	t.Parallel()
	seller, _ := newAccount(t)
	buyer, _ := newAccount(t)
	stranger, _ := newAccount(t)
	d := requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)

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
		requestedOrder(t, buyer.AccessToken, activeListing(t, seller.AccessToken, nil).ID)
	}
	check(t, "a fourth order", dealCall(t, buyer.AccessToken, open.ID, "cod", codBody(home)), 409, "cod_limit_reached")

	check(t, "ship signed out", dealCall(t, "", d.ID, "ship", waybillBody), 401, "unauthenticated")
	check(t, "ship without a key", call(t, "POST", "/v1/deals/"+d.ID+"/ship", waybillBody, bearer(seller.AccessToken)), 400, "")
	check(t, "ship as a stranger", dealCall(t, stranger.AccessToken, d.ID, "ship", waybillBody), 404, "deal_not_found")
	check(t, "buyer ships", dealCall(t, buyer.AccessToken, d.ID, "ship", waybillBody), 409, "move_not_allowed")
	check(t, "ship a bad fee", dealCall(t, seller.AccessToken, d.ID, "ship", map[string]any{"carrier": "spx", "tracking_code": "X", "fee": -1}), 422, "invalid_waybill")

	for _, action := range []string{"deliver", "return"} {
		check(t, action+" signed out", dealCall(t, "", d.ID, action, nil), 401, "unauthenticated")
		check(t, action+" a bad id", dealCall(t, seller.AccessToken, "not-a-uuid", action, nil), 400, "")
		check(t, action+" as a stranger", dealCall(t, stranger.AccessToken, d.ID, action, nil), 404, "deal_not_found")
		check(t, action+" a requested order", dealCall(t, seller.AccessToken, d.ID, action, nil), 409, "move_not_allowed")
	}

	check(t, "orders signed out", call(t, "GET", "/v1/me/orders?role=buying", nil), 401, "unauthenticated")
	check(t, "orders a bad role", call(t, "GET", "/v1/me/orders?role=bogus", nil, bearer(buyer.AccessToken)), 400, "")
}
